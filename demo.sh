#!/bin/bash

# Observo Connector 功能演示脚本

echo "=== Observo Connector 功能演示 ==="
echo

# 设置变量
BINARY="./observo-connector"
SERVER_PORT=8080
CLUSTER_ID="demo-cluster"
ADDRESS="localhost:50051"

# 检查二进制文件是否存在
if [ ! -f "$BINARY" ]; then
    echo "❌ 未找到 observo-connector 二进制文件"
    echo "请先运行: make build"
    exit 1
fi

echo "✅ 找到 observo-connector 二进制文件"
echo

# 函数：显示帮助信息
show_help() {
    echo "=== 查看帮助信息 ==="
    $BINARY --help
    echo
}

# 函数：演示查询功能
demo_query() {
    echo "=== 演示查询功能 ==="
    echo "执行示例查询..."
    
    # 注意：这里使用 --disable-ssl 因为我们可能没有配置真实的Vizier
    $BINARY query "dx.display_name" \
        --cluster-id="$CLUSTER_ID" \
        --address="$ADDRESS" \
        --disable-ssl \
        || echo "⚠️  查询失败 (可能是因为没有连接到真实的Vizier)"
    echo
}

# 函数：演示健康检查
demo_health() {
    echo "=== 演示健康检查 ==="
    echo "检查Vizier集群健康状态..."
    
    $BINARY health \
        --cluster-id="$CLUSTER_ID" \
        --address="$ADDRESS" \
        --disable-ssl \
        || echo "⚠️  健康检查失败 (可能是因为没有连接到真实的Vizier)"
    echo
}

# 函数：启动HTTP服务器
demo_server() {
    echo "=== 启动HTTP API服务器 ==="
    echo "在端口 $SERVER_PORT 启动服务器..."
    echo "按 Ctrl+C 停止服务器"
    echo
    
    # 在后台启动服务器
    $BINARY server \
        --port="$SERVER_PORT" \
        --cluster-id="$CLUSTER_ID" \
        --address="$ADDRESS" \
        --disable-ssl &
    
    SERVER_PID=$!
    
    # 等待服务器启动
    sleep 3
    
    echo "🚀 服务器已启动，PID: $SERVER_PID"
    echo
    
    # 测试API接口
    test_api
    
    # 停止服务器
    echo "停止服务器..."
    kill $SERVER_PID 2>/dev/null
    wait $SERVER_PID 2>/dev/null
    echo "✅ 服务器已停止"
    echo
}

# 函数：测试API接口
test_api() {
    echo "=== 测试API接口 ==="
    
    BASE_URL="http://localhost:$SERVER_PORT"
    
    echo "1. 测试健康检查接口..."
    curl -s "$BASE_URL/api/v1/health?cluster_id=$CLUSTER_ID" | jq . 2>/dev/null || curl -s "$BASE_URL/api/v1/health?cluster_id=$CLUSTER_ID"
    echo
    echo
    
    echo "2. 测试内置metrics接口..."
    curl -s "$BASE_URL/api/v1/metrics"
    echo
    echo
    
    echo "3. 测试查询接口 (GET)..."
    curl -s "$BASE_URL/api/v1/query?query=dx.display_name&cluster_id=$CLUSTER_ID&format=json" | jq . 2>/dev/null || curl -s "$BASE_URL/api/v1/query?query=dx.display_name&cluster_id=$CLUSTER_ID&format=json"
    echo
    echo
    
    echo "4. 测试查询接口 (POST)..."
    curl -s -X POST "$BASE_URL/api/v1/query" \
        -H "Content-Type: application/json" \
        -d '{
            "query": "dx.display_name",
            "cluster_id": "'$CLUSTER_ID'",
            "format": "json"
        }' | jq . 2>/dev/null || curl -s -X POST "$BASE_URL/api/v1/query" \
        -H "Content-Type: application/json" \
        -d '{
            "query": "dx.display_name",
            "cluster_id": "'$CLUSTER_ID'",
            "format": "json"
        }'
    echo
    echo
}

# 函数：显示配置示例
show_config_example() {
    echo "=== 配置文件示例 ==="
    echo "配置文件位置: config.example.yaml"
    echo
    echo "主要配置项："
    echo "- address: Vizier服务器地址"
    echo "- cluster_id: 集群ID"
    echo "- TLS配置: ca_cert, client_cert, client_key"
    echo "- server_port: HTTP服务器端口"
    echo
    cat config.example.yaml 2>/dev/null || echo "❌ 未找到配置文件示例"
    echo
}

# 函数：显示使用场景
show_use_cases() {
    echo "=== 主要使用场景 ==="
    echo
    echo "1. 命令行查询："
    echo "   $BINARY query 'http_events | head(10)' --cluster-id=my-cluster"
    echo
    echo "2. HTTP API服务："
    echo "   $BINARY server --port=8080"
    echo "   curl 'http://localhost:8080/api/v1/query?query=http_events&cluster_id=my-cluster'"
    echo
    echo "3. Prometheus集成："
    echo "   curl 'http://localhost:8080/api/v1/metrics?query=http_events&cluster_id=my-cluster'"
    echo
    echo "4. 数据导出："
    echo "   curl -X POST http://localhost:8080/api/v1/export -d '{\"query\":\"http_events\",\"format\":\"csv\"}'"
    echo
    echo "5. 健康监控："
    echo "   $BINARY health --cluster-id=my-cluster"
    echo
}

# 主菜单
main_menu() {
    while true; do
        echo "=== Observo Connector 演示菜单 ==="
        echo "1. 查看帮助信息"
        echo "2. 演示查询功能"
        echo "3. 演示健康检查"
        echo "4. 启动HTTP服务器并测试API"
        echo "5. 查看配置示例"
        echo "6. 查看使用场景"
        echo "7. 退出"
        echo
        read -p "请选择操作 (1-7): " choice
        
        case $choice in
            1) show_help ;;
            2) demo_query ;;
            3) demo_health ;;
            4) demo_server ;;
            5) show_config_example ;;
            6) show_use_cases ;;
            7) echo "👋 再见!"; exit 0 ;;
            *) echo "❌ 无效选择，请输入 1-7" ;;
        esac
        
        echo
        read -p "按回车键继续..."
        echo
    done
}

# 检查是否安装了必要工具
check_dependencies() {
    echo "=== 检查依赖 ==="
    
    # 检查curl
    if command -v curl >/dev/null 2>&1; then
        echo "✅ curl 已安装"
    else
        echo "⚠️  curl 未安装，API测试可能无法正常工作"
    fi
    
    # 检查jq
    if command -v jq >/dev/null 2>&1; then
        echo "✅ jq 已安装 (用于JSON格式化)"
    else
        echo "⚠️  jq 未安装，JSON输出将不会格式化"
    fi
    
    echo
}

# 主程序
main() {
    echo "🎯 Observo Connector 功能演示"
    echo "这个脚本将演示 observo-connector 的主要功能"
    echo
    
    check_dependencies
    
    if [ "$1" = "--auto" ]; then
        echo "=== 自动演示模式 ==="
        show_help
        demo_query
        demo_health
        show_config_example
        show_use_cases
        echo "✅ 演示完成"
    else
        main_menu
    fi
}

# 运行主程序
main "$@"
