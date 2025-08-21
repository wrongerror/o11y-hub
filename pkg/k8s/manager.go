package k8s

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

// EndpointType represents the type of network endpoint
type EndpointType string

const (
	EndpointTypePod      EndpointType = "pod"
	EndpointTypeService  EndpointType = "service"
	EndpointTypeNode     EndpointType = "node"
	EndpointTypeExternal EndpointType = "external"
	EndpointTypeUnknown  EndpointType = "unknown"
)

// EndpointInfo contains information about a network endpoint
type EndpointInfo struct {
	Type        EndpointType
	Name        string
	Namespace   string
	IP          string
	NodeName    string
	OwnerName   string
	OwnerType   string
	ServiceName string
}

// Manager manages Kubernetes metadata using informers
type Manager struct {
	client kubernetes.Interface
	logger *logrus.Logger
	stopCh chan struct{}
	mutex  sync.RWMutex

	// Caches for quick lookup
	podsByIP     map[string]*EndpointInfo // IP -> EndpointInfo
	podsByName   map[string]*EndpointInfo // namespace/name -> EndpointInfo
	servicesByIP map[string]*EndpointInfo // ClusterIP -> EndpointInfo
	nodesByIP    map[string]*EndpointInfo // NodeIP -> EndpointInfo

	// Informers
	podInformer        cache.SharedIndexInformer
	serviceInformer    cache.SharedIndexInformer
	nodeInformer       cache.SharedIndexInformer
	deploymentInformer cache.SharedIndexInformer
	replicaSetInformer cache.SharedIndexInformer
}

// NewManager creates a new Kubernetes manager
func NewManager(logger *logrus.Logger, kubeconfig string) (*Manager, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		// Use specified kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		// Try default kubeconfig first, then in-cluster config
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			// Fall back to in-cluster config
			config, err = rest.InClusterConfig()
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	manager := &Manager{
		client:       client,
		logger:       logger,
		stopCh:       make(chan struct{}),
		podsByIP:     make(map[string]*EndpointInfo),
		podsByName:   make(map[string]*EndpointInfo),
		servicesByIP: make(map[string]*EndpointInfo),
		nodesByIP:    make(map[string]*EndpointInfo),
	}

	// Create informers
	if err := manager.createInformers(); err != nil {
		return nil, fmt.Errorf("failed to create informers: %w", err)
	}

	return manager, nil
}

// createInformers creates and configures all informers
func (m *Manager) createInformers() error {
	// Pod informer
	m.podInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return m.client.CoreV1().Pods("").List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return m.client.CoreV1().Pods("").Watch(context.TODO(), options)
			},
		},
		&corev1.Pod{},
		30*time.Second,
		cache.Indexers{},
	)

	// Service informer
	m.serviceInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return m.client.CoreV1().Services("").List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return m.client.CoreV1().Services("").Watch(context.TODO(), options)
			},
		},
		&corev1.Service{},
		30*time.Second,
		cache.Indexers{},
	)

	// Node informer
	m.nodeInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return m.client.CoreV1().Nodes().List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return m.client.CoreV1().Nodes().Watch(context.TODO(), options)
			},
		},
		&corev1.Node{},
		30*time.Second,
		cache.Indexers{},
	)

	// Deployment informer
	m.deploymentInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return m.client.AppsV1().Deployments("").List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return m.client.AppsV1().Deployments("").Watch(context.TODO(), options)
			},
		},
		&appsv1.Deployment{},
		30*time.Second,
		cache.Indexers{},
	)

	// ReplicaSet informer
	m.replicaSetInformer = cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return m.client.AppsV1().ReplicaSets("").List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return m.client.AppsV1().ReplicaSets("").Watch(context.TODO(), options)
			},
		},
		&appsv1.ReplicaSet{},
		30*time.Second,
		cache.Indexers{},
	)

	// Add event handlers
	m.addEventHandlers()

	return nil
}

// addEventHandlers adds event handlers to informers
func (m *Manager) addEventHandlers() {
	// Pod event handlers
	m.podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    m.onPodAdd,
		UpdateFunc: m.onPodUpdate,
		DeleteFunc: m.onPodDelete,
	})

	// Service event handlers
	m.serviceInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    m.onServiceAdd,
		UpdateFunc: m.onServiceUpdate,
		DeleteFunc: m.onServiceDelete,
	})

	// Node event handlers
	m.nodeInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    m.onNodeAdd,
		UpdateFunc: m.onNodeUpdate,
		DeleteFunc: m.onNodeDelete,
	})
}

// Start starts all informers
func (m *Manager) Start(ctx context.Context) error {
	m.logger.Info("Starting Kubernetes manager")

	go m.podInformer.Run(m.stopCh)
	go m.serviceInformer.Run(m.stopCh)
	go m.nodeInformer.Run(m.stopCh)
	go m.deploymentInformer.Run(m.stopCh)
	go m.replicaSetInformer.Run(m.stopCh)

	// Wait for cache sync
	if !cache.WaitForCacheSync(m.stopCh,
		m.podInformer.HasSynced,
		m.serviceInformer.HasSynced,
		m.nodeInformer.HasSynced,
		m.deploymentInformer.HasSynced,
		m.replicaSetInformer.HasSynced) {
		return fmt.Errorf("failed to sync caches")
	}

	m.logger.Info("Kubernetes manager started and caches synced")
	return nil
}

// Stop stops all informers
func (m *Manager) Stop() {
	m.logger.Info("Stopping Kubernetes manager")
	close(m.stopCh)
}

// GetEndpointInfo gets endpoint information by IP address
func (m *Manager) GetEndpointInfo(ip string) *EndpointInfo {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Check if it's a Pod IP
	if info, exists := m.podsByIP[ip]; exists {
		return info
	}

	// Check if it's a Service IP
	if info, exists := m.servicesByIP[ip]; exists {
		return info
	}

	// Check if it's a Node IP
	if info, exists := m.nodesByIP[ip]; exists {
		return info
	}

	// Check if it's an external IP
	if m.isExternalIP(ip) {
		return &EndpointInfo{
			Type: EndpointTypeExternal,
			IP:   ip,
		}
	}

	return &EndpointInfo{
		Type: EndpointTypeUnknown,
		IP:   ip,
	}
}

// GetPodOwnerInfo gets owner information for a pod
func (m *Manager) GetPodOwnerInfo(namespace, podName string) (string, string) {
	key := namespace + "/" + podName

	m.mutex.RLock()
	info, exists := m.podsByName[key]
	m.mutex.RUnlock()

	if !exists {
		logrus.Debugf("Pod %s not found in cache. Available pods: %d", key, len(m.podsByName))
		return "", ""
	}

	logrus.Debugf("Found pod %s: owner=%s, type=%s", key, info.OwnerName, info.OwnerType)
	return info.OwnerName, info.OwnerType
}

// Pod event handlers
func (m *Manager) onPodAdd(obj interface{}) {
	pod := obj.(*corev1.Pod)
	m.updatePodCache(pod)
}

func (m *Manager) onPodUpdate(oldObj, newObj interface{}) {
	pod := newObj.(*corev1.Pod)
	m.updatePodCache(pod)
}

func (m *Manager) onPodDelete(obj interface{}) {
	pod := obj.(*corev1.Pod)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Remove from IP cache
	if pod.Status.PodIP != "" {
		delete(m.podsByIP, pod.Status.PodIP)
	}

	// Remove from name cache
	key := pod.Namespace + "/" + pod.Name
	delete(m.podsByName, key)
}

// updatePodCache updates pod information in cache
func (m *Manager) updatePodCache(pod *corev1.Pod) {
	ownerName, ownerType := m.getPodOwner(pod)

	info := &EndpointInfo{
		Type:        EndpointTypePod,
		Name:        pod.Name,
		Namespace:   pod.Namespace,
		IP:          pod.Status.PodIP,
		NodeName:    pod.Spec.NodeName,
		OwnerName:   ownerName,
		OwnerType:   ownerType,
		ServiceName: "",
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Update IP cache
	if pod.Status.PodIP != "" {
		m.podsByIP[pod.Status.PodIP] = info
	}

	// Update name cache
	key := pod.Namespace + "/" + pod.Name
	m.podsByName[key] = info

	logrus.Debugf("Cached pod %s with owner: %s (type: %s)", key, ownerName, ownerType)
}

// getPodOwner gets the owner information for a pod
func (m *Manager) getPodOwner(pod *corev1.Pod) (string, string) {
	if len(pod.OwnerReferences) == 0 {
		logrus.Debugf("Pod %s/%s has no owner references", pod.Namespace, pod.Name)
		return "", ""
	}

	// Get the first owner reference
	owner := pod.OwnerReferences[0]
	ownerName := owner.Name
	ownerType := owner.Kind

	logrus.Debugf("Pod %s/%s direct owner: %s (type: %s)", pod.Namespace, pod.Name, ownerName, ownerType)

	// If owner is a ReplicaSet, try to find the Deployment
	if owner.Kind == "ReplicaSet" {
		if deployment := m.getReplicaSetOwner(pod.Namespace, owner.Name); deployment != "" {
			logrus.Debugf("Found deployment %s for ReplicaSet %s", deployment, owner.Name)
			return deployment, "Deployment"
		}
	}

	return ownerName, ownerType
}

// getReplicaSetOwner gets the deployment that owns a ReplicaSet
func (m *Manager) getReplicaSetOwner(namespace, replicaSetName string) string {
	obj, exists, err := m.replicaSetInformer.GetStore().GetByKey(namespace + "/" + replicaSetName)
	if err != nil || !exists {
		return ""
	}

	rs := obj.(*appsv1.ReplicaSet)
	if len(rs.OwnerReferences) == 0 {
		return ""
	}

	for _, owner := range rs.OwnerReferences {
		if owner.Kind == "Deployment" {
			return owner.Name
		}
	}

	return ""
}

// Service event handlers
func (m *Manager) onServiceAdd(obj interface{}) {
	service := obj.(*corev1.Service)
	m.updateServiceCache(service)
}

func (m *Manager) onServiceUpdate(oldObj, newObj interface{}) {
	service := newObj.(*corev1.Service)
	m.updateServiceCache(service)
}

func (m *Manager) onServiceDelete(obj interface{}) {
	service := obj.(*corev1.Service)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	if service.Spec.ClusterIP != "" && service.Spec.ClusterIP != "None" {
		delete(m.servicesByIP, service.Spec.ClusterIP)
	}
}

// updateServiceCache updates service information in cache
func (m *Manager) updateServiceCache(service *corev1.Service) {
	if service.Spec.ClusterIP == "" || service.Spec.ClusterIP == "None" {
		return
	}

	info := &EndpointInfo{
		Type:        EndpointTypeService,
		Name:        service.Name,
		Namespace:   service.Namespace,
		IP:          service.Spec.ClusterIP,
		ServiceName: service.Name,
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.servicesByIP[service.Spec.ClusterIP] = info
}

// Node event handlers
func (m *Manager) onNodeAdd(obj interface{}) {
	node := obj.(*corev1.Node)
	m.updateNodeCache(node)
}

func (m *Manager) onNodeUpdate(oldObj, newObj interface{}) {
	node := newObj.(*corev1.Node)
	m.updateNodeCache(node)
}

func (m *Manager) onNodeDelete(obj interface{}) {
	node := obj.(*corev1.Node)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Remove all node IPs
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP || addr.Type == corev1.NodeExternalIP {
			delete(m.nodesByIP, addr.Address)
		}
	}
}

// updateNodeCache updates node information in cache
func (m *Manager) updateNodeCache(node *corev1.Node) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP || addr.Type == corev1.NodeExternalIP {
			info := &EndpointInfo{
				Type:     EndpointTypeNode,
				Name:     node.Name,
				IP:       addr.Address,
				NodeName: node.Name,
			}
			m.nodesByIP[addr.Address] = info
		}
	}
}

// isExternalIP checks if an IP is external (not in cluster networks)
func (m *Manager) isExternalIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}

	// Check for private IP ranges
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}

	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network.Contains(parsedIP) {
			return false
		}
	}

	// If not in private ranges, consider it external
	return !parsedIP.IsLoopback() && !parsedIP.IsLinkLocalUnicast()
}
