package main

import (
    "encoding/json"
    "flag"
    "fmt"
    "log"
    "net"
    "net/http"
    "strconv"

    "google.golang.org/grpc"

    // [CẬP NHẬT ĐÚNG TÊN MODULE "consensus"]
    // 1. Import logic Node từ folder pBFT/node
    "consensus/pBFT/node" 

    // 2. Import Proto chung từ folder common/proto
    pb "consensus/common/proto"
)

func main() {
    // --- 1. Parsing Flags ---
    port := flag.String("port", "50051", "gRPC Port")
    id := flag.String("id", "node1", "Node ID")
    flag.Parse()

    // Tính toán port HTTP (Ví dụ: 50051 -> 60051)
    p, _ := strconv.Atoi(*port)
    httpPort := fmt.Sprintf(":%d", p+10000) 

    // --- 2. Topology Configuration ---
    // Danh sách 5 Node trong mạng
    peerMap := map[string]string{
        "node1": "localhost:50051",
        "node2": "localhost:50052",
        "node3": "localhost:50053",
        "node4": "localhost:50054",
        "node5": "localhost:50055",
    }
    // Loại bỏ chính mình khỏi danh sách Peers
    delete(peerMap, *id)

    // --- 3. Init pBFT Server ---
    // Khởi tạo Node với cấu hình mạng
    pbftServer := node.NewServer(*id, peerMap)

    // --- 4. Start gRPC Server ---
    lis, err := net.Listen("tcp", fmt.Sprintf(":%s", *port))
    if err != nil {
        log.Fatalf("Failed to listen: %v", err)
    }

    grpcServer := grpc.NewServer()

    // [QUAN TRỌNG] Đăng ký service với ConsensusService chung
    // Hàm này đến từ file common/proto/consensus_grpc.pb.go
    pb.RegisterConsensusServiceServer(grpcServer, pbftServer)

    // Chạy gRPC trong goroutine để không chặn main thread
    go func() {
        if err := grpcServer.Serve(lis); err != nil {
            log.Fatalf("Failed to serve: %v", err)
        }
    }()

    // --- 5. Connect to Peers ---
    // Đợi một chút để các node khác kịp khởi động rồi kết nối
    go pbftServer.ConnectToPeers()

    // --- 6. HTTP API (Controller) ---
    // Các API này dùng để Dashboard điều khiển Node
    
    // API: Kích hoạt Primary tạo Block mới
    http.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
        if err := pbftServer.StartConsensus(); err != nil {
            http.Error(w, err.Error(), http.StatusBadRequest)
        } else {
            w.WriteHeader(http.StatusOK)
        }
    })

    // API: Reset trạng thái Node về ban đầu
    http.HandleFunc("/reset", func(w http.ResponseWriter, r *http.Request) {
        pbftServer.Reset()
        w.WriteHeader(http.StatusOK)
    })

    // API: Chuyển đổi trạng thái Malicious/Honest
    http.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
        var req struct {
            Action string `json:"action"`
        }
        json.NewDecoder(r.Body).Decode(&req)

        if req.Action == "malicious" {
            pbftServer.SetMalicious(true)
        } else {
            pbftServer.SetMalicious(false)
        }
        w.WriteHeader(http.StatusOK)
    })

    fmt.Printf("pBFT Node %s running (Integrated Mode). gRPC: %s, HTTP: %s\n", *id, *port, httpPort)
    
    // Block main thread bằng HTTP Server
    log.Fatal(http.ListenAndServe(httpPort, nil))
}