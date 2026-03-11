# ⚡ Moodle GIFT Generator

AI-powered web app tạo câu hỏi Moodle GIFT format từ tài liệu của bạn.

## ✨ Tính năng

- **🤖 AI Generate** — Tạo câu hỏi từ văn bản/tài liệu với Claude AI
- **5 loại câu hỏi GIFT**: Trắc nghiệm, Đúng/Sai, Điền từ, Tự luận, Nối cặp
- **📥 Import GIFT** — Upload hoặc paste câu hỏi GIFT có sẵn
- **📚 Ngân hàng câu hỏi** — Lưu, tìm kiếm, quản lý câu hỏi
- **⬇ Export** — Tải file .gift trực tiếp về Moodle
- **🔐 SSO Microsoft 365** — Đăng nhập qua tài khoản tổ chức
- **🐘 PostgreSQL** — Lưu trữ bền vững
- **🐳 Docker** — Triển khai dễ dàng

## 🚀 Cài đặt nhanh

### 1. Clone và cấu hình

```bash
git clone <repo>
cd moodle-gift-generator
cp .env.example .env
# Chỉnh sửa .env với thông tin của bạn
```

### 2. Cấu hình Microsoft 365 SSO

Tạo App Registration tại [Azure Portal](https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps):

1. **New registration** → Đặt tên app
2. **Redirect URI**: `http://localhost:8080/auth/callback` (Web)
3. **Certificates & secrets** → New client secret → Copy value
4. **Overview** → Copy Application (client) ID và Directory (tenant) ID
5. Điền vào `.env`:
   ```
   MS365_CLIENT_ID=<Application ID>
   MS365_CLIENT_SECRET=<Secret Value>
   MS365_TENANT_ID=<Directory ID>
   ```

### 3. Cấu hình Anthropic API

```bash
# Lấy API key tại https://console.anthropic.com
ANTHROPIC_API_KEY=sk-ant-...
```

> **Note**: Nếu không có API key, app chạy ở chế độ demo với câu hỏi mẫu.

### 4. Chạy với Docker Compose

```bash
docker compose up --build
```

Truy cập: **http://localhost:8080**

## 🏗 Kiến trúc

```
┌─────────────────────────────────────────────┐
│                Docker Network               │
│  ┌──────────────┐      ┌─────────────────┐  │
│  │   Go App     │◄────►│   PostgreSQL    │  │
│  │  :8080       │      │   :5432         │  │
│  │              │      └─────────────────┘  │
│  │  ┌────────┐  │                           │
│  │  │Frontend│  │  External:                │
│  │  │HTML/CSS│  │  ┌──────────────────────┐ │
│  │  │  /JS   │  │  │ Microsoft 365 SSO    │ │
│  │  └────────┘  │  │ Anthropic Claude API │ │
│  └──────────────┘  └──────────────────────┘ │
└─────────────────────────────────────────────┘
```

### Tech Stack

| Component | Technology |
|-----------|-----------|
| Backend | Go 1.23 + Chi router |
| Frontend | HTML + CSS + Vanilla JS |
| Database | PostgreSQL 16 |
| Auth | OAuth2 / MS365 SSO |
| AI | Anthropic Claude API |
| Deploy | Docker + Docker Compose |

## 📁 Cấu trúc dự án

```
moodle-gift-generator/
├── cmd/server/main.go          # Entry point
├── internal/
│   ├── auth/auth.go            # MS365 SSO
│   ├── db/db.go                # Database layer
│   ├── handlers/handlers.go   # HTTP handlers
│   ├── models/models.go        # Data models
│   └── services/generator.go  # AI generation
├── frontend/
│   └── templates/
│       ├── login.html          # Login page
│       └── app.html            # Main SPA
├── docker-compose.yml
├── Dockerfile
└── .env.example
```

## 🔌 API Endpoints

| Method | Path | Mô tả |
|--------|------|-------|
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

## 📄 License

MIT
