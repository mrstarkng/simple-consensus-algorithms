# pBFT 

**Lab môn học:** Blockchain và ứng dụng
**Topic:** Practical Byzantine Fault Tolerance (pBFT)



## 1. Giới thiệu Thuật toán pBFT

Dự án này được xây dựng dựa trên bài báo khoa học kinh điển: **"Practical Byzantine Fault Tolerance"** (Miguel Castro & Barbara Liskov, OSDI '99).

### 1.1. Bài toán và Mô hình hệ thống

Hệ thống được thiết kế để hoạt động trong môi trường **không đồng bộ (asynchronous environments)** như Internet, nơi tin nhắn có thể bị trễ, mất, hoặc gửi sai thứ tự.

Mô hình lỗi là **Byzantine Fault Model**: các node lỗi có thể hành xử tùy ý (gửi tin rác, giả mạo, hoặc im lặng). Để đảm bảo tính đúng đắn (**Safety**) và tính sống (**Liveness**), hệ thống cần thỏa mãn công thức tối ưu về số lượng bản sao (replica):

$$N \ge 3f + 1$$

**Trong đó:**
* **$N$**: Tổng số node trong mạng.
* **$f$**: Số lượng node lỗi tối đa mà hệ thống chịu được.

> **Lưu ý:** Lý do cần $N = 3f + 1$ là để đảm bảo sau khi loại bỏ $f$ node không phản hồi, và trừ đi $f$ node lỗi có thể gửi tin giả, số node trung thực còn lại vẫn lớn hơn số node lỗi, đảm bảo đa số quá bán.


### 1.2. Quy trình đồng thuận (Normal-Case Operation)

Khi không có thay đổi Primary (View Change), thuật toán hoạt động theo quy trình 3 pha để đảm bảo mọi node trung thực đều đồng ý cùng một thứ tự thực thi yêu cầu.

#### 1. Giai đoạn Pre-Prepare
* **Client** gửi yêu cầu (Request) $m$ tới node **Primary**.
* **Primary** gán một số thứ tự (Sequence Number) $n$ cho yêu cầu và gửi tin nhắn `<PRE-PREPARE, v, n, d>` cho các Backup, trong đó $v$ là view hiện tại và $d$ là mã băm (digest) của $m$.
* **Backup** chấp nhận tin nhắn này nếu chữ ký hợp lệ và nó chưa từng chấp nhận một $d$ khác cho cùng $v, n$.

#### 2. Giai đoạn Prepare
* Nếu **Backup** chấp nhận Pre-Prepare, nó gửi tin nhắn `<PREPARE, v, n, d, i>` cho tất cả các node khác.
* Một node đạt trạng thái **Prepared** `prepared(m, v, n, i)` khi nó có trong log: request gốc, tin nhắn Pre-Prepare, và $2f$ tin nhắn Prepare hợp lệ từ các node khác. Giai đoạn này đảm bảo các node đồng thuận về thứ tự.

#### 3. Giai đoạn Commit
* Khi đạt trạng thái Prepared, node gửi tin nhắn `<COMMIT, v, n, d, i>` cho tất cả các node khác.
* Một node đạt trạng thái **Committed-Local** khi nó có trạng thái Prepared và nhận được $2f + 1$ tin nhắn Commit (bao gồm cả của chính nó).
* Giai đoạn này đảm bảo rằng nếu một node trung thực commit yêu cầu, thì yêu cầu đó cuối cùng cũng sẽ được commit bởi các node trung thực khác (Safety).

#### 4. Thực thi (Execution)
* Sau khi đạt **Committed-Local**, node thực thi yêu cầu và gửi phản hồi (Reply) trực tiếp cho Client.
* Client chờ $f + 1$ phản hồi giống nhau từ các node khác nhau để chấp nhận kết quả.


##  2. Hướng dẫn Cài đặt & Chạy

### 2.1. Yêu cầu Tiên quyết

* **Go:** v1.19+ ([Tải về](https://go.dev/dl/))
* **Python:** v3.10+ (kèm `pip`) để chạy Test Suite.
* **Protoc Compiler:** Trình biên dịch cho gRPC.
    * **MacOS:** `brew install protobuf`
    * **Linux:** `sudo apt install -y protobuf-compiler`

### 2.2. Thiết lập Môi trường

1.  **Clone dự án:**
    ```bash
    git clone <your-repo-url>
    cd pBFT
    ```

2.  **Cài đặt dependencies:**
    ```bash
    go mod tidy
    pip install requests colorama
    ```

### 2.3. Khởi chạy hệ thống

Sử dụng script tự động để build lại mã nguồn, dọn dẹp tiến trình cũ và chạy 5 nodes + dashboard:

```bash
chmod +x run_network.sh
./run_network.sh
```
Hệ thống sẽ biên dịch và khởi động:

* **Dashboard**: Chạy tại http://localhost:8080

* **5 Node pBFT**: Chạy gRPC tại các port 50051 -> 50055.

### 2.4. Khởi chạy hệ thống trên Window

Dự án hỗ trợ chạy native trên Windows thông qua file Batch script.

1.  **Cài đặt:** Đảm bảo đã cài Go và Protoc (thêm vào PATH).
2.  **Khởi chạy:**
    * Mở Command Prompt (CMD) hoặc PowerShell tại thư mục dự án.
    * Chạy lệnh:
        ```cmd
        run_network.bat
        ```
3.  **Dừng hệ thống:**
    * Nhấn bất kỳ phím nào tại cửa sổ CMD đang chạy để tự động tắt toàn bộ các node và dashboard.

**Lưu ý:** Nếu dùng **Git Bash** trên Windows, bạn có thể chạy trực tiếp file `./run_network.sh` giống như trên Linux/macOS.


## 3. Luồng sử dụng 

Sau khi hệ thống đã khởi chạy thành công, bạn có thể thực hiện các bước sau để quan sát thuật toán pBFT hoạt động:

1.  **Truy cập Dashboard:** Mở trình duyệt tại địa chỉ `http://localhost:8080`.
2.  **Gửi Request:** Nhấn nút "**▶ Client Request**". Dashboard sẽ đóng vai trò Client, gửi lệnh tới Node 1 (Primary).
3.  **Quan sát Đồng thuận:**
    * Xem log real-time ở cột trái để thấy các tin nhắn `PrePrepare`, `Prepare`, `Commit` được trao đổi.
    * Các node sẽ nhấp nháy xanh (nếu **Honest**) hoặc đỏ (nếu **Malicious**).
4.  **Kiểm tra Ledger:** Khi đồng thuận hoàn tất, Block mới sẽ xuất hiện ở bảng "Blockchain Ledger" bên phải với đầy đủ `Idx`, `PrevHash`, `Hash`.
5.  **Giả lập tấn công:**
    * Nhấp vào biểu tượng Node (VD: N5) để chuyển nó sang chế độ **Malicious**.
    * Gửi lại Request để xem hệ thống chống chịu lỗi như thế nào.
6.  **Reset:** Nhấn "**⟳ Reset System**" để xóa Ledger và đưa mọi node về trạng thái ban đầu.