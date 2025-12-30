# RAFT Consensus System

## 1. Tóm tắt thuật toán đồng thuận RAFT
Thuật toán RAFT được thiết kế bởi Diego Ongaro và John Ousterhout tại Đại học Stanford với mục tiêu cốt lõi là sự dễ hiểu và tính ứng dụng thực tế cao hơn so với thuật toán Paxos truyền thống. Cách hoạt động của RAFT dựa trên mô hình "Strong Leader", trong đó một cụm máy tính sẽ bầu ra một thực thể duy nhất đóng vai trò điều hành toàn bộ các thay đổi trạng thái của hệ thống. Quá trình này được phân tách thành ba tiểu vấn đề độc lập: Leader Election, Log Replication và Safety [1]. Khi hệ thống khởi động hoặc Leader hiện tại bị lỗi, các nút sẽ bắt đầu quá trình bầu cử; một ứng viên cần nhận được phiếu bầu từ đa số (Quorum) để trở thành Leader mới nhằm đảm bảo tính toàn vẹn của dữ liệu [2]. 

Mục đích chính của RAFT là cung cấp khả năng chịu lỗi (Crash Fault Tolerance - CFT), giúp hệ thống duy trì sự đồng thuận miễn là đa số các nút vẫn hoạt động bình thường [3]. Tuy nhiên, RAFT cũng tồn tại những hạn chế nhất định. Việc duy trì quyền lực yêu cầu Leader phải gửi các tin nhắn Heartbeat định kỳ, điều này vô tình tạo ra gánh nặng về băng thông khi quy mô cluster tăng lên [2]. Ngoài ra, hiệu năng của hệ thống bị giới hạn bởi độ trễ mạng, do Leader buộc phải đợi xác nhận từ đa số các Follower trước khi có thể cam kết (commit) một bản ghi mới. Trong tình huống mạng bị phân mảnh kéo dài, các nút ở nhóm thiểu số có thể liên tục tăng chỉ số Term, dẫn đến việc gây gián đoạn hệ thống khi các phân mảnh kết nối lại với nhau [1].

## 2. Tính nhất quán và Hạn chế bảo mật của RAFT
Để đảm bảo tính nhất quán tuyệt đối trong quá trình chuyển đổi Leader, RAFT dựa trên thuộc tính "Leader Completeness". Thuộc tính này khẳng định rằng nếu một bản ghi log đã được cam kết ở một Term bất kỳ, thì bản ghi đó chắc chắn sẽ tồn tại trong log của các Leader ở các Term cao hơn [1]. Cơ chế này được thực thi thông qua quy tắc bầu cử nghiêm ngặt: một nút sẽ từ chối bỏ phiếu cho bất kỳ ứng viên nào có log "ít cập nhật hơn" (kém về Term hoặc ngắn hơn về Index) so với log của chính nó [1]. Vì một cuộc bầu cử thành công cần đa số phiếu, và một bản ghi đã commit cũng cần nằm trên đa số nút, nên tập hợp các nút bầu cho Leader mới chắc chắn chứa ít nhất một nút giữ bản ghi đã commit gần nhất, từ đó giúp Leader mới cập nhật lại toàn bộ hệ thống [2].

Mặc dù mạnh mẽ trong việc xử lý các lỗi kỹ thuật, RAFT bộc lộ hạn chế lớn khi đối mặt với các hành vi độc hại (Malicious behavior) hoặc lỗi Byzantine [4]. Bản chất của RAFT là một thuật toán Crash Fault Tolerant (CFT), nó hoạt động dựa trên giả định rằng các nút có thể ngừng hoạt động nhưng nếu chúng gửi tin nhắn, thì nội dung tin nhắn đó luôn trung thực và tuân thủ đúng giao thức [1]. Trong trường hợp một nút bị xâm nhập và cố tình gửi dữ liệu sai lệch, chẳng hạn như Leader gửi các giá trị khác nhau cho các nút khác nhau hoặc giả mạo chỉ số Term, thuật toán RAFT sẽ không thể tự phát hiện và dẫn đến sự sụp đổ của tính nhất quán toàn cục. Để giải quyết các hành vi độc hại này, hệ thống cần áp dụng các giao thức Byzantine Fault Tolerant (BFT) phức tạp hơn, nơi sự đồng thuận không chỉ dựa trên đa số mà còn dựa trên các chứng thực mã hóa và kiểm tra chéo gắt gao [4].

## 3. Mô tả chương trình đã cài đặt

### 3.1 Cấu trúc thư mục (Tổ chức mới)
Dự án được cấu trúc lại để quản lý file chuyên nghiệp hơn:
*   `/dashboard`: Chứa giao diện Web (`index.html`, `wallpaper.jpg`).
*   `/logs`: Thư mục tự động lưu trữ các file trạng thái `storage_0.json` đến `storage_4.json`.
*   `/test`: Chứa bộ công cụ kiểm thử tự động và thư viện Python (`tester.py`, `raft_pb2.py`, `raft_pb2_grpc.py`).
*   `/pb`: Chứa các file gRPC được sinh ra cho ngôn ngữ Go.
*   `main.go`: Mã nguồn Go xử lý logic cốt lõi của thuật toán RAFT.
*   `server.go`: Mã nguồn Web Server điều khiển và giám sát cluster.
*   `raft_node.exe`: File thực thi sau khi biên dịch từ `main.go`.

### 3.2 Hướng dẫn thiết lập và Cài đặt
**Yêu cầu hệ thống:** Go (1.19+), Python (3.10+), Windows (để sử dụng lệnh taskkill tự động).

1.  **Cài đặt thư viện Go:**
    ```bash
    go mod tidy
    go get google.golang.org/grpc
    go get google.golang.org/protobuf
    ```
2.  **Cài đặt thư viện Python - Để chạy kiểm thử (test/tester.py)**
    ```bash
    pip install grpcio grpcio-tools
    ```

### 3.3 Cách chạy chương trình
1.  **Biên dịch:** Tại thư mục gốc, chạy lệnh: `go build -o raft_node.exe main.go`.
2.  **Khởi chạy Server:** Chạy lệnh: `go run server.go`.
3.  **Truy cập:** Mở trình duyệt tại `http://localhost:8080`.

### 3.4 Tính năng và Điều chỉnh tham số
*   **Điều chỉnh số lượng nút:** Mở `main.go` và `server.go`, chỉnh sửa mảng `ports` để thêm hoặc bớt các cổng gRPC (mặc định là 5 node).
*   **Bảng điều khiển (Mission Control Center):**
    *   **Launch All Nodes:** Khởi động đồng loạt 5 node hành tinh.
    *   **Emergency Reset:** Tắt ngay lập tức tất cả các node đang chạy thông qua lệnh `taskkill` trên Windows và đưa UI về trạng thái Standby.
    *   **Set Alpha (Node 1):** Sử dụng RPC đặc biệt để ép Node 1 chiếm quyền Leader cho mục đích Demo.
    *   **Reality Breach:** Giả lập phân mảnh mạng. Khi kích hoạt, một khe nứt không gian sẽ xuất hiện, chia cluster thành 2 phân vùng (Nhóm 0,1 và Nhóm 2,3,4) để quan sát sự mất kết nối và tăng Term.
*   **Tương tác Node:** Người dùng có thể click trực tiếp vào từng hành tinh để "đánh sập" (Offline) hoặc "hồi sinh" (Online) node đó.
*   **Lưu trữ (Persistence):** Mọi lệnh Propose từ Leader sẽ được đồng bộ và lưu vào file `.json` tương ứng trong thư mục `/logs`.

## 4. Tài liệu tham khảo và Đường dẫn trích dẫn
[1] **Ongaro, D., & Ousterhout, J. (2014).** *In Search of an Understandable Consensus Algorithm.* USENIX Annual Technical Conference (ATC). 
[Link gốc](https://www.usenix.org/system/files/conference/atc14/atc14-paper-ongaro.pdf)

[2] **Raft Official Website.** *The Raft Consensus Algorithm.* 
[Link gốc](https://raft.github.io/)

[3] **Howard, H. (2019).** *Distributed Consensus.* Ph.D. Dissertation, University of Cambridge.
[Link gốc](https://www.cl.cam.ac.uk/techreports/UCAM-CL-TR-935.pdf)

[4] **Castro, M., & Liskov, B. (1999).** *Practical Byzantine Fault Tolerance.* OSDI '99: Proceedings of the third symposium on Operating systems design and implementation.
[Link gốc](https://pmg.csail.mit.edu/papers/osdi99.pdf)
