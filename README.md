# Moodle GIFT Quiz Generator

Web app tạo và quản lý câu hỏi Moodle theo chuẩn GIFT format — tự động sinh câu hỏi từ tài liệu hoặc import từ template có sẵn.

## Tính năng chính

- **Tự động sinh câu hỏi** từ văn bản (PDF, DOCX, TXT) — phân tích câu, trích xuất thuật ngữ, áp dụng template theo từng loại GIFT
- **9 loại câu hỏi GIFT**: Trắc nghiệm (MC), Nhiều đáp án (MA), Đúng/Sai (TF), Điền từ (SA), Điền chỗ trống (MW), Nối cặp (MATCH), Câu hỏi số (NUM), Tự luận (ESSAY), Mô tả (DESC)
- **Import template** — Upload file TXT hoặc XLSX theo mẫu có sẵn để chuyển đổi sang GIFT
- **Ngân hàng câu hỏi** — Lưu, tìm kiếm, lọc, xóa hàng loạt, export câu hỏi đã chọn
- **Export GIFT** — Tải file `.txt` tương thích import trực tiếp vào Moodle
- **Xác thực** — Đăng nhập local (admin) hoặc Microsoft 365 SSO
- **Docker** — Triển khai nhanh với Docker Compose

## Cài đặt

### 1. Clone và cấu hình

```bash
git clone <repo>
cd Moodle-Quiz-Generator
cp .env.example .env
```

Chỉnh sửa `.env`:

```env
# Database (mặc định dùng được luôn với Docker Compose)
DB_USER=giftuser
DB_PASSWORD=giftpassword
DB_NAME=giftdb

# Admin password (đổi sau lần đăng nhập đầu)
ADMIN_PASSWORD=admin

# Microsoft 365 SSO (tùy chọn)
MS365_CLIENT_ID=
MS365_CLIENT_SECRET=
MS365_TENANT_ID=
MS365_REDIRECT_URL=http://localhost:8080/auth/callback
```

### 2. Cấu hình Microsoft 365 SSO (tùy chọn)

Tạo App Registration tại [Azure Portal](https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps):

1. **New registration** → Đặt tên app
2. **Redirect URI**: `http://localhost:8080/auth/callback` (Web)
3. **Certificates & secrets** → New client secret → Copy value
4. **Overview** → Copy Application (client) ID và Directory (tenant) ID

### 3. Chạy

```bash
docker compose up --build
```

Truy cập: **http://localhost:8080**

Đăng nhập mặc định: `admin` / `admin`

## Cách sử dụng

### Sinh câu hỏi từ văn bản

1. Mở tab **Tạo câu hỏi**
2. Upload file (PDF, DOCX, TXT) hoặc nhập văn bản trực tiếp
3. Chọn loại câu hỏi cần sinh (MC, TF, SA, Essay, Matching...)
4. Chọn ngân hàng câu hỏi để lưu (tùy chọn)
5. Bấm **Generate** → Câu hỏi hiển thị ở panel bên phải

### Import từ template

1. Tải file mẫu: **Template TXT** hoặc **Template XLSX**
2. Điền câu hỏi theo format mẫu
3. Upload file XLSX → Câu hỏi được chuyển đổi và lưu tự động

### Quản lý ngân hàng câu hỏi

- Tìm kiếm, lọc theo loại câu hỏi
- Chế độ chọn hàng loạt: chọn nhiều câu → Export hoặc Xóa
- Export file `.txt` (GIFT format) để import vào Moodle

## Kiến trúc

```
┌──────────────────────────────────────┐
│            Docker Network            │
│                                      │
│  ┌─────────────┐   ┌─────────────┐  │
│  │  Go Server  │◄─►│ PostgreSQL  │  │
│  │   :8080     │   │   :5432     │  │
│  │             │   └─────────────┘  │
│  │  ┌───────┐  │                    │
│  │  │ SPA   │  │   External:        │
│  │  │ HTML/ │  │   ┌─────────────┐  │
│  │  │ JS/CSS│  │   │  MS365 SSO  │  │
│  │  └───────┘  │   └─────────────┘  │
│  └─────────────┘                    │
└──────────────────────────────────────┘
```

| Component | Technology |
|-----------|------------|
| Backend | Go 1.24, Chi router v5 |
| Frontend | Single-page HTML + Vanilla JS |
| Database | PostgreSQL 16 |
| Auth | Local password + OAuth2 MS365 |
| File parsing | PDF, DOCX, TXT, GIFT, MD (pure Go) |
| Template import | TXT (key-value), XLSX (excelize) |
| Deploy | Docker + Docker Compose |

## Cấu trúc dự án

```
├── cmd/server/main.go              # Entry point, routes
├── internal/
│   ├── auth/auth.go                # Auth middleware, SSO, local login
│   ├── db/db.go                    # PostgreSQL queries
│   ├── handlers/handlers.go        # HTTP handlers
│   ├── models/models.go            # Data models
│   └── services/
│       ├── generator.go            # Rule-based question generation
│       ├── fileparser.go           # PDF/DOCX/TXT text extraction
│       └── templateparser.go       # TXT/XLSX template import
├── frontend/templates/
│   ├── login.html                  # Login page
│   └── app.html                    # Main SPA
├── docker-compose.yml
├── Dockerfile
└── .env.example
```

## API

### Auth

| Method | Path | Mô tả |
|--------|------|--------|
| POST | `/auth/local` | Đăng nhập local |
| GET | `/auth/start` | Bắt đầu OAuth2 MS365 |
| GET | `/auth/callback` | OAuth2 callback |
| GET | `/auth/logout` | Đăng xuất |

### Người dùng

| Method | Path | Mô tả |
|--------|------|--------|
| GET | `/api/me` | Thông tin user hiện tại |
| POST | `/api/change-password` | Đổi mật khẩu |

### Ngân hàng câu hỏi

| Method | Path | Mô tả |
|--------|------|--------|
| GET | `/api/banks` | Danh sách ngân hàng |
| POST | `/api/banks` | Tạo ngân hàng mới |
| DELETE | `/api/banks/{id}` | Xóa ngân hàng |

### Câu hỏi

| Method | Path | Mô tả |
|--------|------|--------|
| GET | `/api/questions` | Danh sách câu hỏi (filter: `bank_id`, `type`, `search`) |
| DELETE | `/api/questions/{id}` | Xóa câu hỏi |
| POST | `/api/questions/bulk-delete` | Xóa hàng loạt |
| POST | `/api/generate` | Sinh câu hỏi từ văn bản |
| POST | `/api/upload` | Upload file, trích xuất text |
| POST | `/api/fetch-url` | Lấy text từ URL |
| POST | `/api/import` | Import GIFT text |
| POST | `/api/import-template` | Import từ template TXT/XLSX |
| GET | `/api/export` | Export GIFT file (.txt) |
| GET | `/api/template/txt` | Tải template TXT mẫu |
| GET | `/api/template/xlsx` | Tải template XLSX mẫu |

## GIFT Question Types

| Type | Ví dụ |
|------|-------|
| **MC** (Multiple Choice) | `::Title::{Q} {=correct ~wrong1 ~wrong2 ~wrong3}` |
| **MA** (Multiple Answer) | `::Title::{Q} {~%50%ans1 ~%50%ans2 ~%-100%wrong}` |
| **TF** (True/False) | `::Title::{Q} {TRUE}` |
| **SA** (Short Answer) | `::Title::{Q} {=answer}` |
| **MW** (Missing Word) | `::Title::Text {=answer} more text.` |
| **MATCH** (Matching) | `::Title::{Q} {=term1 -> def1 =term2 -> def2}` |
| **NUM** (Numerical) | `::Title::{Q} {#42:0.5}` |
| **ESSAY** | `::Title::{Q} {}` |
| **DESC** (Description) | `::Title::{Q}` |


| GET | `/api/me` | Thông tin user hiện tại |
| GET | `/api/banks` | Danh sách bộ câu hỏi |
| POST | `/api/banks` | Tạo bộ mới |
| DELETE | `/api/banks/:id` | Xóa bộ |
| GET | `/api/questions` | Danh sách câu hỏi |
| DELETE | `/api/questions/:id` | Xóa câu hỏi |
| POST | `/api/generate` | AI tạo câu hỏi |
| POST | `/api/import` | Import GIFT |
| POST | `/api/upload` | Upload file |
| GET | `/api/export` | Export GIFT file |

## 📝 GIFT Format Examples

### Multiple Choice
```
::Q1:: Thủ đô của Việt Nam là gì? {
=Hà Nội
~TP. Hồ Chí Minh
~Đà Nẵng
~Cần Thơ
}
```

### True/False
```
::Q2:: Việt Nam nằm ở khu vực Đông Nam Á. { TRUE }
```

### Short Answer
```
::Q3:: Ngôn ngữ lập trình ___ được phát triển bởi Google. { =Go =Golang }
```

### Essay
```
::Q4:: Phân tích ưu và nhược điểm của lập trình hướng đối tượng. {}
```

### Matching
```
::Q5:: Nối các khái niệm OOP với định nghĩa. {
=Encapsulation -> Đóng gói dữ liệu
=Inheritance -> Kế thừa từ class cha
=Polymorphism -> Đa hình
}
```

## 🌐 Production Deployment

```bash
# Thay đổi trong .env:
APP_BASE_URL=https://yourdomain.com
MS365_REDIRECT_URL=https://yourdomain.com/auth/callback
SESSION_SECRET=<32+ random chars>

# Chạy
docker compose -f docker-compose.yml up -d
```

