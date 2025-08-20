package resolver

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// PodInfo contains information about a Kubernetes pod
type PodInfo struct {
	Name        string
	Namespace   string
	ServiceName string
	NodeName    string
	OwnerName   string
	OwnerType   string
	PodType     string
	Labels      map[string]string
	IP          string
	LastSeen    time.Time
}

// AddressResolver resolves IP addresses to Pod information using Kubernetes API
type AddressResolver struct {
	clientset kubernetes.Interface
	logger    *logrus.Logger

	// Cache for IP to Pod mapping
	ipToPod map[string]*PodInfo
	podToIP map[string]string // namespace/podname -> IP
	cacheMu sync.RWMutex

	// Cache TTL
	cacheTTL time.Duration

	// Refresh settings
	refreshInterval time.Duration
	stopCh          chan struct{}
}

// NewAddressResolver creates a new address resolver
func NewAddressResolver(logger *logrus.Logger) (*AddressResolver, error) {
	// Create in-cluster configuration
	config, err := rest.InClusterConfig()
	if err != nil {
		logger.WithError(err).Warn("Failed to create in-cluster config, trying local kubeconfig")

		// Fallback to local kubeconfig for development
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	resolver := &AddressResolver{
		clientset:       clientset,
		logger:          logger,
		ipToPod:         make(map[string]*PodInfo),
		podToIP:         make(map[string]string),
		cacheTTL:        5 * time.Minute,
		refreshInterval: 30 * time.Second,
		stopCh:          make(chan struct{}),
	}

	// Start background refresh
	go resolver.refreshLoop()

	// Initial refresh
	go resolver.refreshPodCache()

	return resolver, nil
}

// ResolvePeer resolves remote address to peer information based on trace role
func (r *AddressResolver) ResolvePeer(localAddr, remoteAddr, localNamespace, localPodName, localService, localNode string, traceRole string) (peerNamespace, peerType, peerAddress, peerPodName, peerServiceName, peerNodeName string) {
	var targetAddr string

	// Determine which address to resolve based on trace role
	if traceRole == "client" {
		// For client, remote is the server we're calling
		targetAddr = remoteAddr
	} else {
		// For server, remote is the client calling us
		targetAddr = remoteAddr
	}

	// Try to resolve the target address
	podInfo := r.resolvePodByIP(targetAddr)

	if podInfo != nil {
		return podInfo.Namespace, podInfo.PodType, targetAddr, podInfo.Name, podInfo.ServiceName, podInfo.NodeName
	}

	// Fallback: try DNS resolution for external services
	if hostname := r.resolveHostname(targetAddr); hostname != "" && hostname != targetAddr {
		return "external", "service", targetAddr, hostname, hostname, "unknown"
	}

	// Unknown/external address
	return "unknown", "unknown", targetAddr, "unknown", "unknown", "unknown"
}

// resolvePodByIP looks up pod information by IP address
func (r *AddressResolver) resolvePodByIP(ip string) *PodInfo {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()

	podInfo, exists := r.ipToPod[ip]
	if !exists {
		return nil
	}

	// Check if cache entry is still valid
	if time.Since(podInfo.LastSeen) > r.cacheTTL {
		return nil
	}

	return podInfo
}

// resolveHostname performs reverse DNS lookup
func (r *AddressResolver) resolveHostname(ip string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	names, err := net.DefaultResolver.LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		return ip
	}

	// Return the first resolved hostname
	hostname := names[0]
	// Remove trailing dot if present
	if strings.HasSuffix(hostname, ".") {
		hostname = hostname[:len(hostname)-1]
	}

	return hostname
}

// refreshLoop runs the cache refresh in background
func (r *AddressResolver) refreshLoop() {
	ticker := time.NewTicker(r.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.refreshPodCache()
		case <-r.stopCh:
			return
		}
	}
}

// refreshPodCache refreshes the pod cache from Kubernetes API
func (r *AddressResolver) refreshPodCache() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get all pods across all namespaces
	pods, err := r.clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		r.logger.WithError(err).Error("Failed to list pods for address resolution")
		return
	}

	// Get all services to map pod to service
	services, err := r.clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		r.logger.WithError(err).Warn("Failed to list services for address resolution")
	}

	// Build service selector map
	serviceMap := make(map[string]map[string]string) // namespace/servicename -> selector
	serviceNames := make(map[string]string)          // namespace/servicename -> actual service name

	if services != nil {
		for _, svc := range services.Items {
			if svc.Spec.Selector != nil {
				key := fmt.Sprintf("%s/%s", svc.Namespace, svc.Name)
				serviceMap[key] = svc.Spec.Selector
				serviceNames[key] = svc.Name
			}
		}
	}

	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()

	// Clear old cache
	r.ipToPod = make(map[string]*PodInfo)
	r.podToIP = make(map[string]string)

	now := time.Now()
	addedCount := 0

	for _, pod := range pods.Items {
		// Skip pods without IP
		if pod.Status.PodIP == "" {
			continue
		}

		// Skip terminated pods
		if pod.Status.Phase == "Succeeded" || pod.Status.Phase == "Failed" {
			continue
		}

		// Find service for this pod
		serviceName := "unknown"
		for svcKey, selector := range serviceMap {
			if r.matchesSelector(pod.Labels, selector) {
				if name, exists := serviceNames[svcKey]; exists {
					// If pod is in same namespace as service
					if strings.HasPrefix(svcKey, pod.Namespace+"/") {
						serviceName = name
						break
					}
				}
			}
		}

		// Determine owner information
		ownerName := "unknown"
		ownerType := "unknown"
		if len(pod.OwnerReferences) > 0 {
			owner := pod.OwnerReferences[0]
			ownerName = owner.Name
			ownerType = strings.ToLower(owner.Kind)
		}

		podInfo := &PodInfo{
			Name:        pod.Name,
			Namespace:   pod.Namespace,
			ServiceName: serviceName,
			NodeName:    pod.Spec.NodeName,
			OwnerName:   ownerName,
			OwnerType:   ownerType,
			PodType:     "pod",
			Labels:      pod.Labels,
			IP:          pod.Status.PodIP,
			LastSeen:    now,
		}

		r.ipToPod[pod.Status.PodIP] = podInfo
		r.podToIP[fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)] = pod.Status.PodIP
		addedCount++
	}

	r.logger.WithFields(logrus.Fields{
		"pods_cached": addedCount,
		"total_pods":  len(pods.Items),
	}).Debug("Refreshed pod address cache")
}

// matchesSelector checks if pod labels match service selector
func (r *AddressResolver) matchesSelector(podLabels, selector map[string]string) bool {
	if len(selector) == 0 {
		return false
	}

	for key, value := range selector {
		if podValue, exists := podLabels[key]; !exists || podValue != value {
			return false
		}
	}

	return true
}

// Stop stops the background refresh
func (r *AddressResolver) Stop() {
	close(r.stopCh)
}

// GetCacheStats returns cache statistics
func (r *AddressResolver) GetCacheStats() (ipCount, podCount int) {
	r.cacheMu.RLock()
	defer r.cacheMu.RUnlock()
	return len(r.ipToPod), len(r.podToIP)
}
