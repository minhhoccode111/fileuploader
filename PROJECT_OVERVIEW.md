# PROJECT OVERVIEW — File Uploader (The Odin Project)

> Tài liệu tham chiếu cho CV/Resume. Toàn bộ nhận định dưới đây được rút ra từ code, cấu hình và
> git history của repository tại thời điểm khảo sát (2026-11-25), có cập nhật bổ sung cho phần
> deploy/CI-CD và seed users (2026-11-26). Chỗ nào code không trả lời được thì ghi rõ
> `[CẦN XÁC NHẬN]`, không suy đoán.

## Mục lục

1. [Tổng quan dự án](#1-tổng-quan-dự-án)
2. [Tính năng chính](#2-tính-năng-chính)
3. [Tech stack](#3-tech-stack)
4. [Kiến trúc hệ thống](#4-kiến-trúc-hệ-thống)
5. [Cấu trúc thư mục](#5-cấu-trúc-thư-mục)
6. [Thiết kế dữ liệu](#6-thiết-kế-dữ-liệu)
7. [API / Giao tiếp](#7-api--giao-tiếp)
8. [Xác thực, phân quyền & bảo mật](#8-xác-thực-phân-quyền--bảo-mật)
9. [Hiệu năng & khả năng mở rộng](#9-hiệu-năng--khả-năng-mở-rộng)
10. [DevOps & chất lượng code](#10-devops--chất-lượng-code)
11. [Thử thách kỹ thuật & giải pháp](#11-thử-thách-kỹ-thuật--giải-pháp)
12. [Số liệu định lượng từ codebase](#12-số-liệu-định-lượng-từ-codebase)
13. [Đóng góp & vai trò](#13-đóng-góp--vai-trò)
14. [Điểm nổi bật tiềm năng cho CV](#14-điểm-nổi-bật-tiềm-năng-cho-cv)
15. [Từ khóa kỹ thuật (ATS keywords)](#15-từ-khóa-kỹ-thuật-ats-keywords)
16. [Hạn chế & hướng cải thiện](#16-hạn-chế--hướng-cải-thiện)
17. [Câu hỏi cần bổ sung](#17-câu-hỏi-cần-bổ-sung)

---

## 1. Tổng quan dự án

**Tên dự án:** File Uploader — bài tập "file uploader" của The Odin Project, được làm lại bằng Go.

**Mục đích:** Xây một "personal drive" thu nhỏ — người dùng tạo tài khoản, được admin duyệt, rồi
lưu trữ file trong các thư mục lồng nhau; upload file lớn theo từng phần; chia sẻ thư mục qua link
công khai; admin quản lý tài khoản, quota và tốc độ truyền.

**Bài toán giải quyết:**

- File lớn không thể gửi trong một request form. Dự án giải bài toán upload nhiều phần (multipart),
  có tiến trình, retry, abort và dọn dẹp phần dở dang.
- Metadata file phải nhất quán với object trong object storage. Dự án tách metadata (Postgres) khỏi
  bytes (S3-compatible bucket) và giữ tính nhất quán bằng transaction + compensation.
- Nhiều người dùng chung một instance cần giới hạn dung lượng và băng thông theo từng tài khoản.
  Dự án dùng quota reservation và bộ đếm Redis dùng chung.
- Giao diện phải mượt như SPA nhưng không cần SPA. Dự án dùng server-rendered templ + htmx +
  Alpine, không có Node ở runtime.

**Đối tượng người dùng:**

- Người dùng cuối: chủ một tài khoản, quản lý drive cá nhân, chia sẻ read-only.
- Admin: duyệt yêu cầu truy cập, sửa tài khoản, reset mật khẩu, đặt quota/rate limit.
- Khách: mở link share công khai, không cần đăng nhập.
- Người xem demo (kế hoạch deploy): dùng account `demo` quota giới hạn, credential hiển thị ở login.

**Loại sản phẩm:** Web application, server-rendered (SSR), monolith. Không phải REST API công khai,
không có mobile app, không có SPA. Giao diện là HTML; một góc JSON nhỏ phục vụ upload driver
(`internal/controller/web/upload.go`).

**Trạng thái:** Phần lớn tính năng đã hoàn thiện và có test. Một số điểm còn để mở:

- `/admin/settings` đã hiển thị các default từ environment (quota, rate, max file size) dạng
  read-only (`internal/controller/web/response/settings.go`).
- Chưa có email verification, self-service password reset.
- Nhiều "known rough edges" được tác giả ghi thẳng trong README (mục _Known rough edges_).
- Dự án tự nhận là learning project (`README.md`); deploy bản demo trên VPS cá nhân.
- **Đã deploy (2026-11-26)** và chạy tại `https://fileuploader.minhhoccode111.com`: VPS cá nhân,
  app chạy Docker Compose (`deploy/docker-compose.prod.yml`), image trên Docker Hub
  `minhhoccode111/fileuploader` (tag `latest` + commit sha), Caddy host-native systemd làm
  reverse proxy với wildcard TLS `*.minhhoccode111.com` qua Cloudflare DNS-01. CI/CD bằng
  GitHub Actions: push `main` → test → build/push image → SSH vào VPS `docker compose pull &&
  up -d` (`.github/workflows/deploy.yml`, key SSH pin `restrict,command=` chỉ chạy được
  `deploy/scripts/ci-deploy.sh`).

**Kế hoạch demo (đã triển khai):** app tự seed 2 account lúc startup qua env `ADMIN_*` và
`DEMO_*` (`internal/app/seed.go`, `usecase.SeedUser`): admin active + demo active quota theo
default 5 GiB; credential demo hiển thị ngay trên màn hình login kèm nút Fill in (Alpine).
Seed idempotent — account theo email đã tồn tại thì skip. Registration vẫn mở nhưng account mới
ở trạng thái `pending`, chưa upload được cho tới khi admin duyệt. Cách này tránh việc nhiều
người đăng ký/upload làm đầy dung lượng VPS.

**Bối cảnh:** Đây là bài tập của The Odin Project; assignment gốc yêu cầu Express + Prisma, tác giả
cố tình làm lại bằng Go để học phần server-side của upload flow (`README.md` dòng 3–6). Lý do chọn
stack: muốn thử **server-side rendering với Go + templ + htmx** thay vì SPA/Node.

---

## 2. Tính năng chính

### 2.1. Tài khoản & kiểm soát truy cập

| Tính năng             | Cách hoạt động                                                                                                                                        | Vị trí code                                                                                                                   |
| --------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| Đăng ký chờ duyệt     | Form đăng ký tạo user với `status = pending`, role `user`; response `202 Accepted` báo "chờ admin duyệt". Root folder được tạo trong cùng transaction | `internal/usecase/user/user.go` (`Register`), `internal/controller/web/register.go`                                           |
| Seed account lúc start | Env `ADMIN_*`/`DEMO_*` → `usecase.SeedUser` tạo admin active + demo active kèm root folder trong UoW; email tồn tại thì skip (idempotent)            | `internal/app/seed.go`, `internal/usecase/user/user.go` (`SeedUser`), `config/config.go` (`Seed`)                              |
| Đăng nhập / đăng xuất | Kiểm tra bcrypt, throttle 2 bucket, tạo session 32 byte ngẫu nhiên, set cookie HttpOnly                                                               | `internal/usecase/user/user.go` (`Login`), `internal/controller/web/login.go`, `internal/controller/web/logout.go`            |
| Duyệt tài khoản       | Admin chuyển `pending → active`; từ chối/khóa chuyển `disabled`                                                                                       | `internal/controller/web/admin.go`, `internal/usecase/user/user.go` (`ApproveUser`, `SetStatus`, `DisableUser`, `EnableUser`) |
| Tự sửa profile        | Đổi username/email khi đang đăng nhập                                                                                                                 | `internal/controller/web/account.go`, `internal/usecase/user/user.go` (`UpdateProfile`)                                       |
| Đổi mật khẩu          | Giữ session hiện tại, kết thúc mọi session khác                                                                                                       | `internal/usecase/user/user.go` (`ChangePassword`)                                                                            |
| Role & trạng thái     | `admin`/`user`, `pending`/`active`/`disabled`; DB có CHECK constraint                                                                                 | `internal/entity/user.go`, migrations `add_user_role`, `add_user_status`                                                      |
| Vô hiệu hóa session   | Đổi role kết thúc mọi session; đổi password giữ session hiện tại; disable kết thúc tất cả                                                             | `internal/usecase/user/user.go`, `internal/repo/session/session.go`                                                           |

### 2.2. Drive (thư mục & file)

| Tính năng                                       | Cách hoạt động                                                                                                      | Vị trí code                                                                                                                |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| Cây thư mục lồng nhau                           | Mỗi account có một root folder (`parent_id IS NULL`); depth tối đa 20                                               | `internal/entity/folder.go`, `internal/usecase/folder/folder.go`                                                           |
| Tạo / rename / move / duplicate / delete folder | Kiểm tra tên trùng, chu trình, depth; khóa row để tránh race                                                        | `internal/usecase/folder/folder.go`                                                                                        |
| Rename / move / duplicate / delete file         | Object key giữ nguyên khi rename/move; duplicate dùng `CopyObject`                                                  | `internal/usecase/file/file.go`                                                                                            |
| Tải file                                        | Stream qua app để áp rate limit; `Content-Disposition` có cả ASCII fallback và RFC 5987                             | `internal/controller/web/file.go`, `internal/controller/web/download.go`, `internal/usecase/file/file.go` (`OpenDownload`) |
| Tải folder dạng ZIP                             | Stream ZIP, giữ đường dẫn tương đối trong cây                                                                       | `internal/usecase/folder/folder.go` (`DownloadZip`, `writeZip`)                                                            |
| Breadcrumb                                      | Truy vấn recursive để dựng đường dẫn từ root                                                                        | `internal/repo/persistent/folder/folder.go` (`Breadcrumb`)                                                                 |
| Chống tên nguy hiểm                             | `CleanName` chặn slash, ký tự Windows cấm, control/bidi, trailing dot, tên thiết bị Windows (`NUL.txt`, `LPT9.log`) | `internal/entity/folder.go`                                                                                                |

### 2.3. Duyệt & tìm kiếm

| Tính năng           | Cách hoạt động                                                         | Vị trí code                                                                                 |
| ------------------- | ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| Phân trang folder   | 100 entry/trang, folders trước files, keyset cursor (không offset)     | `internal/usecase/folder/folder.go` (`browse`), `internal/entity/folder.go` (`EntryCursor`) |
| Lọc trong folder    | `q` chỉ tìm trong direct children, không đổi breadcrumb/folder path    | `internal/usecase/folder/folder.go` (`List`)                                                |
| Tìm kiếm toàn drive | `/search`, tối thiểu 3 ký tự, dùng `pg_trgm`, mỗi kết quả kèm path     | `internal/usecase/folder/folder.go` (`Search`), `internal/entity/search.go`                 |
| Sắp xếp & cursor    | `lower(name)` rồi `id`; cursor base64 JSON có kind/name/id/foldersDone | `internal/entity/folder.go`, `internal/repo/persistent/{folder,file}`                       |
| Fuzz cursor         | Fuzzer cho decoder cursor                                              | `internal/entity/fuzz_test.go`, `make test-fuzz`                                            |

### 2.4. Upload nhiều phần (chunked upload)

| Tính năng           | Cách hoạt động                                                                                      | Vị trí code                                                                   |
| ------------------- | --------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| Bắt đầu upload      | `POST /htmx/uploads` trả JSON part plan `{upload_id, part_size, part_count}`                        | `internal/controller/web/upload.go`                                           |
| Gửi từng part       | `PUT /htmx/uploads/:upload/parts/:part`, kiểm tra đúng kích thước, đẩy qua pacer, lưu ETag          | `internal/usecase/file/upload.go` (`UploadPart`)                              |
| Hoàn tất            | `POST .../complete` join parts, ghi row trong transaction, cập nhật dung lượng                      | `internal/usecase/file/upload.go` (`CompleteUpload`)                          |
| Hủy                 | `POST .../abort` abort multipart, xóa session, giải phóng quota                                     | `internal/usecase/file/upload.go` (`AbortUpload`)                             |
| Idempotent complete | Marker `upload-completed:<id>` giữ 1 giờ, retry trả lại file đã tạo                                 | `internal/repo/upload/upload.go`, `internal/usecase/file/upload.go`           |
| Quota reservation   | Reserve toàn bộ size khai báo khi start; complete/abort/sweeper giải phóng                          | `internal/usecase/file/upload.go` (`reserveUploadQuota`)                      |
| Sweeper             | Chạy lúc start và mỗi 5 phút; abort session hết hạn và multipart mồ côi > 24h                       | `internal/app/sweeper.go`, `internal/usecase/file/upload.go` (`SweepUploads`) |
| Client driver       | XHR gửi tuần tự, progress, retry 3 lần với exponential backoff + jitter, abort bằng keepalive fetch | `web/static/js/app.js`                                                        |

### 2.5. Chia sẻ công khai

| Tính năng          | Cách hoạt động                                               | Vị trí code                                                           |
| ------------------ | ------------------------------------------------------------ | --------------------------------------------------------------------- |
| Tạo share          | Một share/folder; tạo lại cập nhật expiry và giữ token cũ    | `internal/usecase/share/share.go` (`Create`)                          |
| Expiry             | `expires_at` NULL = không hết hạn; hết hạn trả 404 với khách | `internal/entity/share.go`, `internal/usecase/share/share.go` (`Get`) |
| Thu hồi            | Xóa row; link chết ngay                                      | `internal/usecase/share/share.go` (`Delete`)                          |
| Duyệt trong share  | Chỉ folder con của folder được share; check subtree          | `internal/usecase/share/share.go` (`ResolveFolder`, `ResolveFile`)    |
| Download qua share | File public vẫn chịu rate limit của owner                    | `internal/controller/web/share.go`, `internal/usecase/file/file.go`   |
| Trang quản lý      | `/share` liệt kê share đang active, có drawer sửa expiry/xóa | `web/templates/share.templ`, `internal/controller/web/share.go`       |

### 2.6. Admin

| Tính năng            | Cách hoạt động                                                                              | Vị trí code                                                                                  |
| -------------------- | ------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Danh sách tài khoản  | Skeleton + htmx load bảng; search, filter role/status/ngày, sort mọi cột, phân trang offset | `internal/controller/web/admin.go`, `web/templates/admin.templ`                              |
| Sửa tài khoản        | Đổi username/email/role/password, set quota/rate, đổi status                                | `internal/controller/web/admin.go` (`adminUsersEditSubmit`)                                  |
| Duyệt access request | Danh sách `pending`, approve từng tài khoản                                                 | `internal/controller/web/admin.go` (`adminAccessRequestsTable`, `adminAccessRequestApprove`) |
| Reset mật khẩu       | Admin đặt mật khẩu mới, session cũ bị kết thúc                                              | `internal/usecase/user/user.go` (`UpdateUser`)                                               |
| Giới hạn             | Quota, upload rate, download rate; 0 = kế thừa global default. Bảng users và drawer edit hiển thị giá trị **hiệu dụng** (resolve default), input vẫn giữ giá trị lưu | `internal/entity/user.go` (`Effective*`), `internal/controller/web/response/users.go` (`UserDefaults`), `config/config.go` |
| Settings             | Read-only: các default từ environment (quota, rates, max file size) để admin config user đúng | `internal/controller/web/pages.go` (`adminSettings`), `response/settings.go`, `web/templates/admin.templ` |

### 2.7. UI/UX

- htmx 4 xử lý link, form, partial swap; Alpine 3 chỉ điều khiển drawer và file picker
  (`web/templates/layout.templ`, `web/static/js/app.js`).
- Drawer upload có progress bar, toast, lỗi inline; đóng drawer hoặc `pagehide` sẽ abort
  (`web/static/js/app.js`).
- Tên dài được rút gọn giữa bằng canvas measurement + binary search
  (`web/static/js/app.js`, hàm `truncateMiddle`).
- Loading skeleton cho bảng admin; `HX-Trigger` để reload bảng sau khi role đổi
  (`web/templates/admin.templ`, `internal/controller/web/admin.go`).
- Filter admin đẩy URL bằng `HX-Push-Url` để bookmark được
  (`internal/controller/web/admin.go`).
- Login page hiển thị credential demo (khi cấu hình `DEMO_*`) kèm nút "Fill in" tự điền form
  bằng Alpine `$refs` (`web/templates/login.templ`, `templates.View.DemoLogin`).
- Favicon SVG tự vẽ (glyph upload trắng trên nền indigo) phục vụ từ `/static`
  (`web/static/favicon.svg`, `web/templates/layout.templ`).

### 2.8. Vận hành & quan sát

- Request ID xuyên suốt proxy → app → log (`internal/controller/web/middleware/requestid.go`).
- Prometheus metrics: request count, duration histogram, response bytes, in-flight, build info,
  Postgres pool (`pkg/metrics/metrics.go`).
- pprof trên private listener (`internal/app/observe.go`).
- Sweeper dọn upload dở (`internal/app/sweeper.go`).

---

## 3. Tech stack

### 3.1. Ngôn ngữ & runtime

| Thành phần           | Phiên bản | Vai trò                                            | Nguồn                           |
| -------------------- | --------- | -------------------------------------------------- | ------------------------------- |
| Go                   | 1.27      | Ngôn ngữ chính, toàn bộ backend                    | `go.mod` dòng 3                 |
| JavaScript (vanilla) | —         | Upload driver, drawer, truncation; không framework | `web/static/js/app.js`          |
| templ                | v0.3.1100 | Template engine type-safe, compile ra Go           | `go.mod`                        |
| HTML/CSS             | —         | Server-rendered pages                              | `web/templates/`, `web/app.css` |

### 3.2. Backend framework & thư viện

| Thư viện                 | Phiên bản | Vai trò                      | Nguồn                                     |
| ------------------------ | --------- | ---------------------------- | ----------------------------------------- |
| Fiber                    | v2.52.15  | HTTP framework trên fasthttp | `go.mod`, `pkg/httpserver/server.go`      |
| fasthttp                 | v1.73.0   | HTTP engine bên dưới Fiber   | `go.mod`                                  |
| templ                    | v0.3.1100 | Render HTML component        | `go.mod`, `web/templates/`                |
| go-playground/validator  | v10.30.5  | Validate DTO                 | `go.mod`, `pkg/validatorx/`               |
| caarlos0/env             | v11.4.1   | Parse config từ env          | `go.mod`, `config/config.go`              |
| Squirrel                 | v1.5.4    | SQL builder, không ORM       | `go.mod`, `internal/repo/persistent/`     |
| pgx                      | v5.11.0   | Postgres driver + pool       | `go.mod`, `pkg/postgres/`                 |
| golang-migrate           | v4.20.1   | Migration up/down            | `go.mod`, `migrations/`                   |
| go-redis                 | v9.23.0   | Redis client                 | `go.mod`, `pkg/redis/`                    |
| minio-go                 | v7.4.1    | S3-compatible client         | `go.mod`, `pkg/minio/`                    |
| zerolog                  | v1.35.1   | Structured JSON logging      | `go.mod`, `pkg/logger/logger.go`          |
| google/uuid              | v1.6.0    | ID generation                | `go.mod`                                  |
| x/crypto (bcrypt)        | v0.58.0   | Password hashing             | `go.mod`, `internal/usecase/user/user.go` |
| x/sync (errgroup)        | v0.24.0   | Server lifecycle             | `pkg/httpserver/server.go`                |
| prometheus/client_golang | v1.25.0   | Metrics                      | `go.mod`, `pkg/metrics/`                  |

### 3.3. Dữ liệu & hạ tầng

| Thành phần                | Phiên bản          | Vai trò                                                                                                                                                       | Nguồn                               |
| ------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------- |
| PostgreSQL                | 18.6-alpine        | Metadata: users, folders, files, shares                                                                                                                       | `docker-compose.yml`                |
| Redis                     | 8.10.2-alpine      | Session, upload session, quota lock, rate bucket, login throttle                                                                                              | `docker-compose.yml`                |
| MinIO (fork `pgsty/silo`) | RELEASE.2026-09-16 | S3-compatible object storage cho bytes                                                                                                                        | `docker-compose.yml`                |
| nginx                     | 1.31.3-alpine      | Reverse proxy cho stack local, `X-Real-IP`, `X-Request-ID`, body limit 110m                                                                                   | `nginx/nginx.conf`                  |
| Caddy (production plan)   | Chưa có trong repo | Host-native systemd trên VPS; wildcard TLS `*.minhhoccode111.com` qua Cloudflare DNS-01; set `X-Real-IP`/`X-Forwarded-For`/`X-Forwarded-Proto` trong snippet  | Repo `devops/caddy` (ngoài project) |
| Docker / Compose          | —                  | Đóng gói và chạy stack                                                                                                                                        | `Dockerfile`, `docker-compose.yml`  |
| Message queue             | Không dùng         | Công việc nền chỉ là goroutine sweeper, không có broker                                                                                                       | `internal/app/sweeper.go`           |
| Hosting / Cloud           | Chưa deploy        | Dự kiến VPS Hostinger; app ở `fileuploader.minhhoccode111.com`; Cloudflare proxied, SSL Full (Strict); Docker Compose; image trên Docker Hub `minhhoccode111` | Kế hoạch deploy, chưa có trong repo |

### 3.4. Frontend

| Thành phần   | Phiên bản | Vai trò                          | Nguồn                           |
| ------------ | --------- | -------------------------------- | ------------------------------- |
| htmx         | 4.0.0     | AJAX partial swap, form, polling | `Makefile` (`HTMX_VERSION`)     |
| Alpine.js    | 3.17.4    | Drawer state, file picker        | `Makefile` (`ALPINE_VERSION`)   |
| Tailwind CSS | 4.3.3     | Utility CSS                      | `Makefile` (`TAILWIND_VERSION`) |
| daisyUI      | 5.7.50    | Component classes                | `Makefile` (`DAISYUI_VERSION`)  |

### 3.5. Build, test, chất lượng

| Công cụ                   | Phiên bản | Vai trò                   | Nguồn                          |
| ------------------------- | --------- | ------------------------- | ------------------------------ |
| air                       | 1.67.4    | Hot reload Go + templ     | `go.mod` (`tool`), `.air.toml` |
| golangci-lint             | 2.14.0    | 47 linters + 4 formatters | `go.mod`, `.golangci.yml`      |
| gofumpt / gci / goimports | —         | Format & import grouping  | `.golangci.yml`, `Makefile`    |
| govulncheck               | 1.9.0     | Quét lỗ hổng dependency   | `Makefile`, `go.mod`           |
| Atlas                     | 1.3.0     | Dump schema               | `Makefile`                     |
| miniredis                 | 2.40.0    | Redis giả cho unit test   | `go.mod`                       |
| GitHub Actions            | —         | CI 7 job + deploy tự động | `.github/workflows/{ci,deploy}.yml` |

---

## 4. Kiến trúc hệ thống

### 4.1. Kiểu kiến trúc

**Monolith + layered architecture theo hướng Clean/Hexagonal (ports & adapters).**

- Một binary duy nhất (`cmd/app`), một HTTP transport duy nhất (`internal/controller/web`).
- Luồng phụ thuộc một chiều:

```text
cmd/app
  └─ internal/app                  composition root + lifecycle
       ├─ internal/controller/web  transport (Fiber)
       │    └─ internal/usecase    business rules
       │         └─ internal/repo  contracts + implementations
       └─ pkg                      infrastructure không biết domain
```

- `internal/entity` là lõi domain, không import package nào trong module.
- `internal/usecase/contracts.go` định nghĩa interface cho controller.
- `internal/repo/contracts.go` định nghĩa interface cho use case, gồm cả `UnitOfWork`,
  `ObjectStorage`, `TransferLimiter`, `LoginGuard`.
- `internal/app/app.go` là nơi duy nhất wire concrete implementation (`initUseCases`,
  `initServers`).
- Không có ORM; SQL build bằng Squirrel, transaction do `pkg/postgres` quản lý.

**Đặc điểm nổi bật:**

- **Unit of Work**: `repo.UnitOfWork.Do(ctx, fn)` mở transaction và nhét transaction vào context.
  Repository dùng `r.Querier(ctx)` nên tự tham gia transaction mà không cần thêm method
  (`internal/repo/contracts.go`, `pkg/postgres/tx.go`).
- **Compensation cho object storage**: Postgres transaction không bao trùm MinIO. Use case tự
  abort multipart / xóa object khi transaction fail (`internal/usecase/file/upload.go`,
  `internal/usecase/file/file.go`).
- **Ports & adapters**: `ObjectStorage` chỉ biết key, không biết domain; `TransferLimiter` và
  `LoginGuard` che giấu Redis.
- **Server-rendered**: controller trả HTML fragment; htmx quyết định swap. Không có JSON API
  tổng quát.

### 4.2. Sơ đồ thành phần

```mermaid
flowchart TD
    B[Browser<br/>htmx + Alpine + XHR] -->|HTTP| F[Fiber router<br/>middleware chain]
    F --> W[internal/controller/web<br/>handlers + DTO]
    W --> U[internal/usecase<br/>user / folder / file / share / pace]
    U --> RC[internal/repo contracts]
    RC --> PG[(PostgreSQL)]
    RC --> RD[(Redis)]
    RC --> S3[(MinIO / S3)]
    U --> P[usecase/pace<br/>rate-limited reader]
    P --> RD
    APP[internal/app<br/>wiring + sweeper + observe] -.-> F
    APP -.-> U
    M[pkg/metrics + pprof] -.-> APP
```

### 4.3. Luồng request điển hình

```mermaid
sequenceDiagram
    participant B as Browser
    participant MW as Middleware chain
    participant G as Auth guard
    participant H as Handler
    participant U as Use case
    participant R as Repository
    participant DB as Postgres/Redis

    B->>MW: GET /folders/:folder
    MW->>MW: RequestID → Logger → Metrics → Recovery → Latency → CSRF → BodyLimit
    MW->>G: RequireAuth
    G->>DB: GET session:<id>
    G-->>H: locals session + userID
    H->>H: parse query, validate DTO
    H->>U: List(ctx, userID, folderID, q, cursor)
    U->>R: Get + Breadcrumb + ListPage
    R->>DB: SQL
    R-->>U: rows
    U-->>H: FolderView
    H-->>B: HTML fragment (hoặc full page)
```

### 4.4. Luồng upload nhiều phần

```mermaid
sequenceDiagram
    participant JS as app.js
    participant API as Fiber handler
    participant R as Redis
    participant S3 as MinIO
    participant PG as Postgres

    JS->>API: POST /htmx/uploads {folder, name, size}
    API->>R: LockQuota + đọc used/reserved
    API->>S3: CreateMultipartUpload
    API->>R: Create session + reserve size
    API-->>JS: {upload_id, part_size, part_count}

    loop mỗi part
        JS->>API: PUT /htmx/uploads/:id/parts/:n
        API->>R: Get session + kiểm tra size
        API->>S3: UploadPart (paced)
        API->>R: SetPart(etag) + slide TTL
        API-->>JS: 204
    end

    JS->>API: POST /htmx/uploads/:id/complete
    API->>R: Parts + LockQuota
    API->>S3: CompleteMultipartUpload
    API->>PG: INSERT files + UPDATE storage_used (UoW)
    API-->>JS: fragment rows + HX-Trigger
```

### 4.5. Cách các thành phần giao tiếp

- **Browser ↔ App:** HTTP/HTML; htmx gửi request với `X-CSRF-Token`; response có thể là fragment,
  full page, redirect `HX-Location`, hoặc status code.
- **App ↔ Postgres:** pgx pool, Squirrel builder, transaction qua Unit of Work.
- **App ↔ Redis:** session, upload state, quota lock, rate bucket, login throttle; nhiều chỗ dùng
  Lua script để atomic.
- **App ↔ MinIO:** multipart API, `CopyObject`, `GetObject`, `RemoveObject`, `ListIncompleteUploads`.
- **App ↔ Metrics:** Prometheus registry nội bộ, serve trên listener riêng.
- **App ↔ reverse proxy (local):** nginx set `X-Real-IP`, `X-Request-ID`, giới hạn body 110m.
- **App ↔ reverse proxy (production plan):** Caddy host-native systemd đứng trước app container;
  app ở `fileuploader.minhhoccode111.com`; wildcard TLS `*.minhhoccode111.com` lấy qua Cloudflare
  DNS-01; Cloudflare SSL mode **Full (Strict)**; snippet mẫu set `X-Real-IP`, `X-Forwarded-For`,
  `X-Forwarded-Proto`. Khi Cloudflare proxied, `{remote_host}` là IP Cloudflare; muốn throttle theo IP
  thật phải trust Cloudflare ranges và dùng `{client_ip}` hoặc `CF-Connecting-IP`.

---

## 5. Cấu trúc thư mục

```text
.
├── cmd/app/                     entry point: đọc config, gọi app.Run
├── config/                      parse env bằng caarlos0/env; validate part size
├── internal/
│   ├── app/                     wiring, lifecycle, migrate, sweeper, observe
│   ├── controller/web/          Fiber handlers, router, middleware, DTO
│   │   ├── middleware/          RequestID, Logger, Metrics, Recovery, Latency, CSRF, BodyLimit, guard
│   │   ├── request/             DTO đầu vào + validate tag
│   │   └── response/            DTO đầu ra cho template
│   ├── entity/                  domain types, sentinel errors, cursor, CleanName, search
│   ├── repo/
│   │   ├── contracts.go         interface cho persistence/storage/limiter/guard/UoW
│   │   ├── persistent/          Postgres repos: user, folder, file, share
│   │   ├── session/             Redis session repo
│   │   ├── transfer/            Redis rate limiter + login guard
│   │   └── upload/              Redis multipart upload session repo
│   └── usecase/
│       ├── contracts.go         interface cho controller
│       ├── user/                account rules
│       ├── folder/              folder rules, zip, search
│       ├── file/                file rules, upload, download
│       ├── share/               public link rules
│       └── pace/                io.Reader có rate limit
├── pkg/                         infrastructure không biết domain
│   ├── httpserver/              Fiber server + graceful shutdown
│   ├── logger/                  zerolog wrapper + request ID
│   ├── metrics/                 Prometheus registry + collectors
│   ├── minio/                   S3 client, bucket, multipart helpers
│   ├── multipart/               Part type dùng chung
│   ├── postgres/                pool, querier, UnitOfWork
│   ├── redis/                   client wrapper
│   └── validatorx/              custom validator + error extraction
├── migrations/                  7 cặp migration up/down
├── web/
│   ├── templates/               10 file .templ, 76 component
│   ├── static/css/app.css       Tailwind output (committed)
│   ├── static/js/app.js         hand-written browser code
│   ├── static/js/{htmx,alpine}.min.js  vendor pinned
│   ├── plugins/daisyui.mjs      vendor pinned
│   └── app.css                  Tailwind input
├── nginx/nginx.conf             reverse proxy config
├── deploy/                      production stack cho VPS (compose prod, .env.example, script)
├── docs/
│   ├── schema.sql               schema dump từ DB
│   └── superpowers/             design specs + implementation plans
├── .github/workflows/ci.yml     CI 7 job, chạy trên push main
├── .github/workflows/deploy.yml  test → build/push Docker Hub → ssh deploy
├── Dockerfile                   multi-stage → scratch
├── docker-compose.yml           5 service
├── Makefile                     37 target
├── AGENTS.md                    project conventions (30 KB)
└── README.md                    tài liệu chính (589 dòng)
```

**Vai trò từng phần:**

- `cmd/app`: điểm vào duy nhất; đọc config, khởi động `app.Run`.
- `internal/app`: composition root. Không chứa business rule. Có `migrate.go` chỉ build với tag
  `migrate`, `sweeper.go`, `observe.go`.
- `internal/controller/web`: transport. Mỗi domain một file handler (`user`, `drive`, `folder`,
  `file`, `share`, `admin`, `account`, `upload`, `search`).
- `internal/usecase`: business rule. Không biết HTTP. Trả sentinel error.
- `internal/repo`: persistence. Postgres cho metadata; Redis cho session/upload/rate; MinIO cho bytes.
- `internal/entity`: domain type + invariant + sentinel error. Không import nội bộ.
- `pkg`: hạ tầng tái sử dụng, không import `internal/`.
- `web/templates`: page + fragment. `*_templ.go` được commit.
- `migrations`: SQL up/down; app tự apply khi build tag `migrate`.

---

## 6. Thiết kế dữ liệu

### 6.1. Tổng quan bảng

Có **4 bảng nghiệp vụ** (+ `schema_migrations` do golang-migrate tạo):

| Bảng      | Vai trò                                     | Khóa chính | Ghi chú                                              |
| --------- | ------------------------------------------- | ---------- | ---------------------------------------------------- |
| `users`   | Tài khoản, role, status, quota, rate, usage | `id UUID`  | unique `email`, `username`; CHECK cho role/status    |
| `folders` | Cây thư mục tự tham chiếu                   | `id UUID`  | `parent_id` NULL = root; FK cascade                  |
| `files`   | Metadata file                               | `id UUID`  | `storage_key` unique; FK `folder_id`, `user_id`      |
| `shares`  | Link công khai tới folder                   | `id UUID`  | unique `(user_id, folder_id)`; `expires_at` nullable |

### 6.2. Quan hệ

```mermaid
erDiagram
    users ||--o{ folders : owns
    users ||--o{ files : owns
    users ||--o{ shares : creates
    folders ||--o{ folders : parent_of
    folders ||--o{ files : contains
    folders ||--o{ shares : shared_as

    users {
        uuid id PK
        varchar username UK
        varchar email UK
        varchar password_hash
        varchar role
        varchar status
        bigint storage_quota_bytes
        bigint storage_used_bytes
        bigint upload_rate_bytes_per_sec
        bigint download_rate_bytes_per_sec
        timestamp created_at
        timestamp updated_at
    }
    folders {
        uuid id PK
        uuid user_id FK
        uuid parent_id FK
        varchar name
        timestamp created_at
        timestamp updated_at
    }
    files {
        uuid id PK
        uuid user_id FK
        uuid folder_id FK
        varchar name
        varchar content_type
        varchar storage_key UK
        bigint size
        timestamp created_at
        timestamp updated_at
    }
    shares {
        uuid id PK
        uuid user_id FK
        uuid folder_id FK
        timestamp expires_at
        timestamp created_at
        timestamp updated_at
    }
```

### 6.3. Index & ràng buộc đáng chú ý

| Index / constraint                                                          | Mục đích                                                   | Nguồn                                                        |
| --------------------------------------------------------------------------- | ---------------------------------------------------------- | ------------------------------------------------------------ |
| `users_email_key`, `users_username_key`                                     | Chống trùng tài khoản                                      | `migrations/20260403000001_create_users.up.sql`              |
| `users_role_check`, `users_status_check`                                    | DB-level enum validation                                   | migrations `add_user_role`, `add_user_status`                |
| `folders_root_key` (partial unique `WHERE parent_id IS NULL`)               | Mỗi user chỉ có một root                                   | `migrations/20261024000001_create_folders_and_files.up.sql`  |
| `folders_sibling_name_key` (`user_id, parent_id, lower(name)`)              | Tên folder không trùng trong cùng parent, case-insensitive | cùng migration                                               |
| `files_folder_name_key` (`folder_id, lower(name)`)                          | Tên file không trùng trong folder, case-insensitive        | cùng migration                                               |
| `folders_user_name_idx`, `files_user_name_idx` (`user_id, lower(name), id`) | Keyset pagination + sort                                   | `migrations/20261121044558_add_folder_browse_indexes.up.sql` |
| `folders_name_trgm_idx`, `files_name_trgm_idx` (GIN `gin_trgm_ops`)         | Tìm kiếm substring toàn drive                              | cùng migration                                               |
| `shares_user_folder_key` (`user_id, folder_id`)                             | Một share/folder                                           | `migrations/20261107092831_create_shares.up.sql`             |
| `files_storage_key_key`                                                     | Object key duy nhất                                        | migration folders/files                                      |

### 6.4. Quyết định thiết kế

- **Không soft delete.** Xóa là xóa row; FK `ON DELETE CASCADE` dọn subtree. Object được xóa sau
  khi transaction commit (hoặc sau khi row đã mất), vì "object mồ côi" vô hình còn hơn "row trỏ
  vào object không tồn tại" (`internal/usecase/folder/folder.go` `Delete`).
- **Storage key sinh từ file ID** (`storageKey(userID, fileID)` trong `internal/usecase/file/file.go`),
  nên rename/move không đụng tới object.
- **Tên so sánh bằng `lower(name)`** để tránh trùng kiểu `A.txt` và `a.txt`; đồng thời cho phép
  index biểu thức.
- **Unique chỉ trong cùng bảng.** Quy tắc "folder và file không được trùng tên trong cùng folder"
  không có DB constraint; nó nằm ở `checkName` trong use case, dưới khóa row
  (`internal/usecase/folder/folder.go`, `internal/usecase/file/file.go`). Đây là điểm yếu đã được
  README ghi nhận.
- **`storage_used_bytes` do app cập nhật**, không dùng trigger. Delete giảm trước khi xóa bytes;
  duplicate kiểm tra quota trước khi copy (`internal/usecase/file/file.go`).
- **Root folder được tạo trong cùng transaction với user** (`internal/usecase/user/user.go`
  `Register`), đảm bảo mọi account luôn có drive.
- **User mới mặc định `pending`** để chờ admin duyệt: `Register` set status tường minh trong Go, và
  migration `add_user_status` đặt default `pending`. `docs/schema.sql` đang ghi `active` — đây là
  schema dump bị lệch, không phải behavior mong muốn.
- **Role/status là text + CHECK**, không dùng enum, để dễ mở rộng giá trị
  (`migrations/20261019162023_add_user_role.up.sql`).
- **Session không nằm trong DB**, mà ở Redis với index `user-sessions:<user_id>` để thu hồi hàng
  loạt (`internal/repo/session/session.go`).

### 6.5. Luồng dữ liệu xóa folder

1. `FOR UPDATE` khóa user/root.
2. Xóa file rows trong subtree, lấy `storage_key` trả về.
3. Xóa folder row; FK cascade dọn subtree.
4. Xóa object trong bucket (sau khi row đã mất).
5. Nếu xóa object fail, chỉ log `ErrStorageCleanup`; row và quota đã mất.

Nguồn: `internal/usecase/folder/folder.go` (`Delete`, `removeAll`),
`internal/repo/persistent/file/file.go` (`DeleteByFolderSubtree`).

---

## 7. API / Giao tiếp

### 7.1. Chuẩn giao tiếp

- **Không REST/GraphQL/gRPC/WebSocket.** Đây là web app SSR.
- Hầu hết route trả **HTML** (full page hoặc fragment).
- Một góc JSON: `POST /htmx/uploads` trả part plan; các call part/abort trả status code.
- Không versioning API. Không Swagger/OpenAPI. Tài liệu là README + AGENTS.md + code.
- htmx là "hợp đồng": response phải là fragment/redirect/status mà htmx hiểu.

### 7.2. Nhóm endpoint

Tổng cộng **61 route** đăng ký trong `internal/controller/web/router.go`
(37 `GET`, 23 `POST`, 1 `PUT`).

| Nhóm                   | Số route | Ví dụ                                                                                                                                                                                                                  | Ghi chú                              |
| ---------------------- | -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------ |
| Trang công khai / auth | 8        | `GET /`, `/folders/:folder`, `/search`, `/share`, `/account`, `/login`, `/register`, `/logout`                                                                                                                         | Guard `RequireAuth` / `RequireGuest` |
| Public share           | 5        | `GET /share/:share`, `/share/:share/folders/:folder`, `/share/:share/files/:file/download`, 2 htmx entries                                                                                                             | Không cần đăng nhập                  |
| Auth mutation          | 6        | `POST /htmx/login`, `/htmx/register`, `/htmx/logout`, `/htmx/account/profile`, `/htmx/account/password`, `/htmx/admin/role`                                                                                            | Trả fragment/redirect                |
| Share management       | 5        | `GET /htmx/folders/:folder/share`, `POST /htmx/shares`, `POST /htmx/shares/:share/time`, `POST .../delete`                                                                                                             | Owner-scoped                         |
| Drive folders          | 12       | `GET /htmx/folders/tree`, `POST /htmx/folders`, `POST /htmx/folders/:folder/{rename,move,duplicate,delete}`                                                                                                            | Fragment                             |
| Drive files            | 8        | `GET /htmx/files/:file`, `POST /htmx/files/:file/{rename,delete,move,duplicate}`                                                                                                                                       | Fragment                             |
| Upload                 | 4        | `POST /htmx/uploads`, `PUT .../parts/:part`, `POST .../complete`, `POST .../abort`                                                                                                                                     | JSON plan + status code              |
| Download               | 3        | `GET /files/:file/download`, `GET /folders/:folder/download`, `GET /download`                                                                                                                                          | Stream                               |
| Admin                  | 9        | 4 trang: `/admin`, `/admin/users`, `/admin/settings`, `/admin/access-requests`; 5 htmx: `/htmx/admin/users`, `/htmx/admin/users/edit` (GET/POST), `/htmx/admin/access-requests`, `/htmx/admin/access-requests/approve` | `RequireAdmin`                       |
| Health                 | 1        | `GET /healthz`                                                                                                                                                                                                         | 200                                  |

### 7.3. Quy ước request/response

- **Form:** `application/x-www-form-urlencoded`; DTO nằm trong `internal/controller/web/request/`.
- **Upload start:** JSON `{folder, name, size, content_type}`.
- **Upload part:** body stream, bắt buộc `Content-Length`, tối đa `UPLOAD_PART_BYTES`.
- **Download:** `Content-Disposition` có quoted ASCII fallback + `filename*` RFC 5987;
  `X-Content-Type-Options: nosniff`.
- **htmx headers:**
  - `HX-Trigger: entrySaved` / `usersUpdated` / `accessRequestsUpdated` để phần khác reload.
  - `HX-Location` cho redirect (qua `middleware.Redirect`).
  - `HX-Push-Url` cho filter admin (bookmarkable).
  - `HX-Request-Type: partial` để trả fragment + title update.
  - `X-Upload-Folder` cho complete để page biết folder vừa nhận file.
- **CSRF:** mọi request unsafe phải có `X-CSRF-Token` khớp cookie `csrf_token`.

### 7.4. Validation

- Validate cấu trúc bằng `validator.v10` trên DTO (`c.v.Struct(req)`), sau `Sanitize()`.
- Custom validator:
  - `username`: Unicode letter/digit, 3–255.
  - `password`: min 6, có upper/lower/digit/special.
- Domain validation nằm ở use case/entity, không lặp ở controller.
- Ví dụ tag: `validate:"required,uuid4"`, `oneof=admin user`, `max=255`, `min=0`
  (`internal/controller/web/request/*.go`).

### 7.5. Xử lý lỗi

- Domain failure là sentinel trong `internal/entity/errors.go` (32 sentinel).
- Controller map bằng `errors.Is` qua helper (`driveError`, `accountError`, `userUpdateError`,
  `shareError`, `uploadFailure`).
- Lỗi không map được: log kèm request ID, trả 500 generic; không leak chi tiết.
- Status code tiêu biểu:

| Status              | Khi nào                                |
| ------------------- | -------------------------------------- |
| 200                 | Trang/fragment thành công              |
| 202                 | Đăng ký thành công, chờ duyệt          |
| 204                 | Upload part/abort thành công           |
| 302 / `HX-Location` | Redirect sau mutation hoặc guard       |
| 400                 | Cursor không hợp lệ                    |
| 403                 | CSRF token sai/Origin khác             |
| 404                 | Folder/file/share/upload không tồn tại |
| 409                 | Trùng tên, upload thiếu part           |
| 411                 | Upload part không có `Content-Length`  |
| 413                 | File/body vượt giới hạn/quota          |
| 422                 | Validate form/name/depth/cycle         |
| 429                 | Start upload quá nhanh                 |
| 500                 | Lỗi không map                          |
| 503                 | Quota lock bận                         |

---

## 8. Xác thực, phân quyền & bảo mật

### 8.1. Authentication

- **Password hash:** bcrypt cost mặc định (`bcrypt.DefaultCost`) — `internal/usecase/user/user.go`.
- **Session ID:** 32 byte random, base64 RawURL (`crypto/rand`), lưu Redis `session:<id>`
  (`internal/usecase/user/user.go` `newSessionID`, `internal/repo/session/session.go`).
- **Cookie:** `access_token`, HttpOnly, SameSite=Lax, `Secure` khi `ctx.Protocol() == "https"`;
  TTL tuyệt đối 24h (`SESSION_TTL`), activity không gia hạn.
- **Session payload:** `{user_id, role}`; guard đọc Redis, không query Postgres mỗi request
  (`internal/entity/session.go`, `middleware/guard.go`).
- **Logout:** xóa session Redis + clear cookie (`internal/controller/web/logout.go`).

### 8.2. Authorization

- Role: `admin`, `user`; status: `pending`, `active`, `disabled`.
- Guard theo route, không theo group:
  - `RequireAuth`: chưa đăng nhập → `/login`.
  - `RequireAdmin`: không phải admin → `/`.
  - `RequireGuest`: đã đăng nhập → `/`.
- Mọi query drive (folder/file) đều filter `user_id`; file/folder của account khác trả "not found"
  thay vì 403 (tránh lộ sự tồn tại).
- Share công khai được resolve theo `shareID`, rồi owner lấy từ row share (`share.UserID`), không
  lấy từ client.
- Subtree check khi mở folder/file trong share (`internal/usecase/share/share.go`
  `folderInSubtree`).

### 8.3. Các biện pháp bảo mật đã triển khai

| Biện pháp          | Chi tiết                                                                                          | Nguồn                                                              |
| ------------------ | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| CSRF double-submit | Cookie 32 byte + header `X-CSRF-Token`, so sánh constant-time; kiểm tra `Origin` host             | `middleware/csrf.go`                                               |
| Login throttling   | 5 lần/email + 30 lần/IP trong 15 phút; reserve atomic bằng Lua; reset email bucket khi thành công | `internal/usecase/user/user.go`, `internal/repo/transfer/login.go` |
| Password policy    | Min 6, upper/lower/digit/special; xử lý bcrypt 72-byte limit                                      | `pkg/validatorx/`, `internal/usecase/user/user.go`                 |
| Input sanitize     | `CleanName`, `CleanSearch`, trim/lower email, validate UUID                                       | `internal/entity/folder.go`, `internal/entity/search.go`           |
| Tenant isolation   | Filter `user_id` ở repo drive; share resolve owner từ row; không tin ID từ client                 | `internal/repo/persistent/*`                                       |
| Share subtree      | Chỉ folder/file trong subtree của share                                                           | `internal/usecase/share/share.go`                                  |
| Body limit         | 1 MiB cho route thường; `partSize + 1 MiB` cho part path; từ chối chunked body ngoài upload       | `middleware/bodylimit.go`                                          |
| Request ID an toàn | Chỉ nhận `[A-Za-z0-9._-]{8,64}`, còn lại sinh UUID                                                | `middleware/requestid.go`                                          |
| Bucket private     | Không public policy; bytes stream qua app                                                         | `README.md`, `pkg/minio/bucket.go`                                 |
| Secret qua env     | Không hard-code; `.env.example` chỉ là default local                                              | `config/config.go`, `.env.example`                                 |
| SQL injection      | Squirrel parameterized; sort field whitelist qua enum                                             | `internal/entity/user.go`, `internal/repo/persistent/user/user.go` |
| Path traversal     | `CleanName` chặn `/`, `\`, `..`, control chars                                                    | `internal/entity/folder.go`                                        |
| Zip path           | Relative path tính từ cây folder, không lấy từ input                                              | `internal/usecase/folder/folder.go` `relativePaths`                |
| CORS               | Không bật; app same-origin, không có API cross-origin                                             | `internal/controller/web/router.go` (không có middleware CORS)     |

### 8.4. Điểm yếu bảo mật đã biết (tác giả ghi trong README)

- Disable account không chặn public share link; share resolution không đọc status owner.
- Không có guard "admin cuối cùng"; admin có thể tự demote/disable.
- Registration không throttle; email đã tồn tại trả 409 (enumeration oracle).
- Password-change trả 422 khi sai current password (oracle).
- Cookie `Secure` phụ thuộc `ctx.Protocol()`; nginx bundled không set `X-Forwarded-Proto`, nên sau
  TLS termination cookie có thể thiếu `Secure`. Với Caddy, Caddy tự set header này; SSL mode đã chốt
  **Full (Strict)** nên Cloudflare → origin là HTTPS và app thấy `https`, nhưng vẫn nên kiểm tra
  cookie thực tế sau deploy.
- Access log dùng `ctx.IP()` (địa chỉ proxy), trong khi login throttle tin `X-Real-IP`. Snippet Caddy
  mẫu có set `X-Real-IP {remote_host}`, nhưng khi Cloudflare proxied thì `{remote_host}` là IP
  Cloudflare; muốn throttle theo IP thật phải thêm Cloudflare ranges vào `trusted_proxies` và dùng
  `{client_ip}` hoặc `CF-Connecting-IP`. Nếu snippet thiếu header, mọi khách chung một IP bucket;
  compose publish port 8090 nên client trực tiếp có thể xoay header.
- CSRF token không rotate khi login/logout.
- Unknown email trả sớm trước bcrypt → timing side channel (message giống nhau nhưng thời gian khác).
- Share token chính là UUID share ID; không phải token riêng có scope/rotation.
- `Storage cleanup` fail chỉ log; có thể drift quota.

---

## 9. Hiệu năng & khả năng mở rộng

### 9.1. Pagination & query

- **Keyset cursor**, không offset, cho folder page: `ORDER BY lower(name), id` với composite index
  `(user_id, lower(name), id)` (`internal/repo/persistent/folder/folder.go`,
  `migrations/20261121044558_add_folder_browse_indexes.up.sql`).
- Mỗi repo page lấy `limit + 1` để biết còn trang sau mà không cần `COUNT`
  (`internal/usecase/folder/folder.go` `browse`).
- **Hai pass**: folders trước, files sau; cursor mang `kind`, `name`, `id`, `foldersDone`.
- **Global search** dùng `pg_trgm` GIN; yêu cầu ≥ 3 ký tự để index được dùng
  (`internal/usecase/folder/folder.go` `Search`).
- **Breadcrumb/Paths** dùng recursive CTE, một query cho nhiều folder
  (`internal/repo/persistent/folder/folder.go`).
- **Admin list** dùng offset pagination (khác drive), có `LIMIT/OFFSET` và `COUNT`
  (`internal/repo/persistent/user/user.go`).

### 9.2. Streaming

- Download file stream qua app, `WriteTimeout = 0` để không cắt file lớn
  (`pkg/httpserver/server.go`).
- Download folder zip stream bằng `io.Pipe`, không buffer cả archive
  (`internal/usecase/folder/folder.go` `writeZip`).
- Upload part đọc stream, không đọc cả file vào RAM (`internal/controller/web/upload.go`
  `BodyStream`).
- `DisablePreParseMultipartForm: true`, `StreamRequestBody: true` trong Fiber config
  (`pkg/httpserver/server.go`).

### 9.3. Rate limiting & quota

- **Per-account transfer limiter** dùng Redis key `upload-rate:<user>` / `download-rate:<user>`.
- Thuật toán: virtual next-send pointer, cập nhật atomic bằng Lua:
  `newNext = max(now, current) + bytes/rate` (`internal/repo/transfer/transfer.go`).
- `pace.Reader` đọc theo batch 64 KiB–1 MiB, reserve cả batch
  (`internal/usecase/pace/reader.go`).
- **Upload quota reservation**: reserve toàn bộ size khi start, dùng `used + reserved + size > quota`
  để chặn hai upload cùng giành headroom (`internal/usecase/file/upload.go`).
- **Quota lock**: Redis lock 30s, renew 10s, chờ tối đa 10s rồi trả 503
  (`internal/repo/upload/upload.go`).
- **Start rate limit**: 20 upload start/phút/account (`internal/usecase/file/upload.go`
  `_startLimit`).
- **Part size thích ứng**: `rate * 90s`, clamp 5–50 MiB, để part vừa timeout proxy/server
  (`partSizeForRate`).

### 9.4. Caching & session

- Session/role cache trong Redis, guard không chạm Postgres (`middleware/guard.go`).
- Rate bucket tự hết hạn sau 1h không dùng (`_rateTTL`).
- Upload session TTL trượt theo part; sweep khi idle (`UPLOAD_SESSION_IDLE_TTL=1h`).
- Không cache HTML/page; không CDN.

### 9.5. Concurrency

- **Row lock**: mọi mutation drive lấy `FOR UPDATE` trên root folder của account
  (`LockUser`) — đơn giản, tuần tự hóa ghi trong một account
  (`internal/repo/persistent/folder/folder.go`).
- **Unit of Work** cho multi-row writes (`pkg/postgres/tx.go`).
- **S3 compensation**: abort multipart / remove object khi row fail
  (`internal/usecase/file/upload.go`, `internal/usecase/file/file.go`).
- **Background sweeper**: một goroutine, tick 5 phút (`internal/app/sweeper.go`).
- **Graceful shutdown** qua `errgroup` + signal (`pkg/httpserver/server.go`,
  `internal/app/app.go`).

### 9.6. Khả năng mở rộng

- App **stateless về session** (Redis), nên có thể chạy nhiều instance sau load balancer.
- Rate limiter và quota lock dùng Redis → chia sẻ giữa instance.
- Tuy nhiên:
  - `HTTP_USE_PREFORK_MODE=true` tắt observe listener (mỗi worker tranh port).
  - Fake latency mặc định 100–200ms/request.
  - Row lock trên root folder giới hạn throughput ghi của một account (chấp nhận được với
    personal drive).
  - Sweeper chạy trong mỗi process; nhiều instance sẽ cùng sweep (idempotent nhưng tốn công).
- Không có queue/message broker; công việc nền chỉ là sweeper.

### 9.7. Hiện trạng đo lường

- Đã test local với file **6 GB** (theo người dùng xác nhận), nằm trong giới hạn mặc định 10 GiB.
- **Chưa đo throughput, p95 latency hay request/s**; chưa có benchmark/load test. Mọi phát biểu về
  hiệu năng trong tài liệu này là suy ra từ thiết kế, không phải số đo.

---

## 10. DevOps & chất lượng code

### 10.1. Docker

- **Multi-stage build** (`Dockerfile`):
  1. `golang:1.27-alpine3.23` modules cache.
  2. Builder: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags migrate`.
  3. Runtime: `scratch`, copy binary + config + migrations + static + CA certs.
- Image runtime không có shell, không có Node; static assets đã build sẵn và commit.
- `-tags migrate` bật `internal/app/migrate.go`, tự retry DB 20 lần và apply migration khi start.

### 10.2. Docker Compose

5 service (`docker-compose.yml`):

| Service | Image                  | Port                 | Ghi chú                             |
| ------- | ---------------------- | -------------------- | ----------------------------------- |
| `db`    | `postgres:18.6-alpine` | 5433:5432            | volume `db_data`, DB `fileuploader` |
| `redis` | `redis:8.10.2-alpine`  | 6379                 | `maxmemory 256mb`, `noeviction`     |
| `minio` | `docker.io/pgsty/silo` | 9000/9001            | S3 + console                        |
| `app`   | build local            | 8090, 127.0.0.1:9090 | limit 1 CPU / 2 GB                  |
| `nginx` | `nginx:1.31.3-alpine`  | 80                   | reverse proxy `app.lvh.me`          |

- App container chạy `OBSERVE_ADDR=0.0.0.0:9090`; compose publish `127.0.0.1:9090:9090`.
- `depends_on` không healthcheck; app tự retry migration.

**Deploy thật (đã triển khai 2026-11-26):** repo dùng nginx cho stack local; trên VPS dùng **Caddy
host-native systemd** (repo `devops/caddy`, snippet `fileuploader.minhhoccode111.com` set
`X-Real-IP`/`X-Forwarded-For`/`X-Forwarded-Proto`, `request_body max_size 64MB` cho upload part),
reverse proxy về `127.0.0.1:8090`; app + Postgres + Redis + MinIO chạy bằng
`deploy/docker-compose.prod.yml` tại `~/fileuploader` (không nginx, không publish port db/redis/
minio, app bind loopback). Quản lý: `deploy/scripts/{00_init,ci-deploy,logs}.sh`; secret thật sinh
random nằm trong `.env` (0600) trên VPS, không trong repo.

Bằng chứng Caddy: `devops/caddy/Caddyfile` khối `*.minhhoccode111.com` dùng
`tls { dns cloudflare {env.CLOUDFLARE_API_TOKEN} }`; `devops/caddy/snippets/upload.minhhoccode111.com`
set `X-Real-IP {remote_host}`, `X-Forwarded-For {remote_host}`, `X-Forwarded-Proto {scheme}`. Lưu ý:
`trusted_proxies static private_ranges` trong `Caddyfile` chưa gồm Cloudflare ranges, nên `{client_ip}`
vẫn là IP Cloudflare nếu không bổ sung. Snippet cho app (`fileuploader.minhhoccode111.com`) chưa có
trong repo Caddy.

### 10.3. CI/CD

Hai workflow, cùng trigger `push` vào `main`:

**`.github/workflows/ci.yml` (7 job, chất lượng):**

1. `golangci-lint` (timeout 15m).
2. `yamllint` (reporter `github-check`).
3. `hadolint` (Dockerfile).
4. `dotenv-linter`.
5. `test`: `go test -race ./...` + `go test -shuffle=on -count=5 ./...`.
6. `integration`: start Compose `db` + `minio`, chờ Postgres, `make test-integration`.
7. `check-dependencies`: `go list -json -m all` + Nancy (continue-on-error).

**`.github/workflows/deploy.yml` (deploy tự động):**

1. `test`: `go test -race ./...` làm cổng.
2. `release`: docker buildx build (cache `type=gha`) → push Docker Hub `minhhoccode111/fileuploader`
   với 2 tag `latest` + commit sha → SSH (key pin trong `authorized_keys` bằng
   `restrict,command="/home/mhc/fileuploader/scripts/ci-deploy.sh"`, port 2222, `known_hosts`
   pin) → script trên VPS chạy `docker compose pull app && up -d app && image prune`.
3. `concurrency` cho nhóm deploy — hai lần push không đè nhau.

Secret GitHub (7): `DOCKERHUB_USERNAME/TOKEN`, `VPS_HOST/PORT/USER`, `VPS_SSH_PRIVATE_KEY`,
`VPS_KNOWN_HOSTS`. Rollback: set `IMAGE_TAG=<sha>` trong `.env` trên VPS để freeze image.
App tự áp migration lúc start (build tag `migrate`) nên deploy không cần bước migrate riêng.

```mermaid
flowchart LR
    U[push main] --> CI[ci.yml: lint + test + integration]
    U --> D[deploy.yml: test -&gt; build -&gt; Docker Hub -&gt; ssh deploy]
    D --> VPS[VPS: docker compose pull + up -d]
    VPS --> CAD[Caddy systemd<br/>fileuploader.minhhoccode111.com]
```

- Caddy host-native systemd đứng trước app container; app không publish trực tiếp ra Internet.
- Wildcard cert `*.minhhoccode111.com` lấy qua **Cloudflare DNS-01** (plugin `caddy-dns/cloudflare`);
  token nằm ở `/etc/caddy/.env` (0600), systemd override nạp qua `EnvironmentFile`.
- Snippet Caddy `fileuploader.minhhoccode111.com` đã deploy trên VPS, `request_body max_size 64MB`
  đủ cho part 50 MiB.

**Checklist deploy (đã thực hiện):**

- Secret VPS sinh random (Postgres, MinIO), `.env` 0600, không commit.
- `LOG_LEVEL=info`, `HTTP_FAKE_LATENCY_MAX=0s`.
- App publish `127.0.0.1:8090:8090` + `127.0.0.1:9090:9090`; db/redis/minio không publish port.
- Seed `ADMIN_*`/`DEMO_*`; registration giữ `pending`.
- Backup volume Postgres/MinIO và monitor disk: **chưa có** — mục 16.4 vẫn còn mở.

### 10.4. Logging

- zerolog JSON ra stdout (`pkg/logger/logger.go`).
- Access log: request ID, method, path, status, size, latency
  (`internal/controller/web/middleware/logger.go`).
- Handler error log dùng cùng request ID (`c.requestLogger(ctx)`).
- `/healthz` và `/static/` không log nhưng vẫn tính metrics.

### 10.5. Monitoring

- Prometheus metrics trên listener riêng `OBSERVE_ADDR` (mặc định `127.0.0.1:9090`):
  - `http_requests_total{method,route,status}`
  - `http_request_duration_seconds{method,route}`
  - `http_response_bytes_total{method,route}`
  - `http_requests_in_flight`
  - `app_build_info{version}`
  - `db_pool_conns{state}`
- Label dùng **route template** (`/folders/:folder`), unmatched là `route="unmatched"` để tránh
  cardinality nổ (`pkg/metrics/metrics.go`, `middleware/metrics.go`).
- pprof: `/debug/pprof/`, `cmdline`, `profile`, `symbol`, `trace` (`internal/app/observe.go`).
- Không có tracing (OTel), không có dashboard/alert.

### 10.6. Linting & formatting

- 47 linters + 4 formatters (`.golangci.yml`).
- Các linter "khó tính" đáng chú ý:
  - `wsl_v5`, `nlreturn`: bắt buộc blank line quanh block/return.
  - `funlen` 65 dòng/40 statement, `gocyclo` 10, `gocognit` 15.
  - `mnd`: cấm magic number.
  - `gochecknoglobals`, `gochecknoinits`.
  - `exhaustive`: switch enum phải đủ case/default.
  - `dupl` threshold 100.
  - `err113`: cấm `errors.New`/`fmt.Errorf` động; phải dùng sentinel.
- `make format` chạy `go fix`, `gofumpt`, `gci`.
- `make pre-commit` gộp deps, generate, assets, CSS, format, schema, test, lint.

### 10.7. Testing

| Loại                       | Công cụ                               | Số lượng / ghi chú                                      | Nguồn                                   |
| -------------------------- | ------------------------------------- | ------------------------------------------------------- | --------------------------------------- |
| Unit/HTTP/integration test | `testing` chuẩn                       | 470 `Test*` functions (gồm 7 file integration) + 1 fuzz | `*_test.go`                             |
| HTTP/controller test       | Fiber `app.Test` + fake use case/repo | 14 file test trong `internal/controller/web`            | `internal/controller/web/*_test.go`     |
| Middleware test            | fake observer, httptest               | 8 file                                                  | `middleware/*_test.go`                  |
| Redis test                 | `miniredis`                           | session, upload, transfer                               | `internal/repo/*`                       |
| Postgres/MinIO integration | build tag `integration`               | 7 file, cần Docker                                      | `internal/repo/persistent/integration/` |
| Fuzz                       | `go test -fuzz`                       | cursor decoder 10s                                      | `internal/entity/fuzz_test.go`          |
| Race                       | `go test -race`                       | CI + Make target                                        | `Makefile`                              |
| Shuffle                    | `go test -shuffle=on -count=10`       | kiểm tra test độc lập                                   | `Makefile`                              |
| Coverage                   | `go test -coverprofile`               | 39.4% statement (unit, không tính integration)          | đo tại 2026-11-25                       |

- Không dùng framework/assertion library ngoài stdlib.
- Fake hand-written, không gomock/mockery (`docs/superpowers/specs/2026-11-22-meaningful-test-suite-design.md`).
- Integration test dùng DB `fileuploader_test` và bucket `fileuploader-test-*`, không đụng dev data.
- Coverage thấp ở repo layer vì phần lớn hành vi DB nằm sau build tag `integration`.

### 10.8. Tài liệu

- `README.md` 589 dòng: setup, config, kiến trúc, bài học, known rough edges.
- `AGENTS.md` ~30 KB: convention, handler contract, dependency rule, lệnh Makefile.
- `docs/superpowers/specs/` 5 design spec; `docs/superpowers/plans/` 5 implementation plan.
- `docs/schema.sql` dump schema.
- `CLAUDE.md` trỏ về `AGENTS.md`.
- `.opencode/skills/` chứa skill hướng dẫn templ/htmx/Alpine/daisyUI.
- Quy trình review: **AI review → open-code-review (ocr) → bạn đọc review cuối**; không có human
  reviewer khác (theo mô tả).

---

## 11. Thử thách kỹ thuật & giải pháp

### 11.1. Upload file lớn qua nhiều request

**Vấn đề:** 10 GiB không thể đi trong một form POST; proxy/server timeout; mất kết nối giữa
chừng; cần biết tiến trình; không được để lại object rác.

**Giải pháp:**

- Multipart upload của S3: start → part → complete/abort (`internal/usecase/file/upload.go`).
- Part plan JSON để browser biết `part_size`, `part_count` (`internal/controller/web/upload.go`).
- Mỗi part kiểm tra đúng kích thước kỳ vọng (`ExpectedPartSize`), lưu ETag vào Redis.
- Complete idempotent bằng marker `upload-completed:<id>` (1h).
- Abort/complete race được xử lý bằng quota lock + đọc lại session dưới lock.
- Sweeper dọn session hết hạn và multipart mồ côi > 24h (`internal/app/sweeper.go`).
- Client retry 3 lần với exponential backoff + jitter; abort bằng keepalive fetch
  (`web/static/js/app.js`).

**Lý do:** S3 multipart là cách chuẩn để upload lớn; Redis giữ state nhẹ, TTL tự dọn; sweeper là
backstop cho browser đóng tab.

**Đánh đổi:** Part gửi tuần tự; complete fail giữa chừng phải upload lại từ đầu; reservation có
thể chặn quota đến khi sweep.

### 11.2. Rate limit theo tài khoản, chia sẻ giữa nhiều stream

**Vấn đề:** Một account có thể mở nhiều download/upload song song; nếu mỗi stream tự ngủ theo
tốc độ riêng thì tổng băng thông vượt giới hạn.

**Giải pháp:**

- Một bucket Redis/account/direction (`upload-rate:<user>`, `download-rate:<user>`).
- Lua script atomic: đọc next-send, đẩy lên `max(now,next) + bytes/rate`, trả `sendAt`.
- `pace.Reader` reserve theo batch 64 KiB–1 MiB, nên số lần gọi Redis ít.
- Download file, folder zip, share download đều đi qua cùng bucket của owner.

**Lý do:** Virtual scheduling cho phép nhiều process/instance chia sẻ một budget mà không cần
central coordinator; Lua đảm bảo không race.

**Đánh đổi:** Reservation bị tính cả khi client disconnect; batch đầu tiên gửi gần như miễn phí.

Nguồn: `internal/repo/transfer/transfer.go`, `internal/usecase/pace/reader.go`.

### 11.3. Keyset pagination cho hai loại entry

**Vấn đề:** Folder page trộn folders và files, muốn "folders trước, files sau", mỗi trang 100,
không dùng offset (chậm và lệch khi dữ liệu đổi).

**Giải pháp:**

- Hai repo query riêng, mỗi query `ORDER BY lower(name), id`, lấy `limit+1`.
- Cursor base64 JSON `{kind, name, id, foldersDone}`; page sau resume đúng pass.
- Nếu folder pass đầy trang, cursor đánh dấu `foldersDone` và trang sau chỉ đọc files.
- Index composite `(user_id, lower(name), id)` cho cả hai bảng.
- Fuzz decoder vì cursor là dữ liệu client sửa được.

**Lý do:** Keyset ổn định khi dữ liệu thay đổi, dùng được index, không cần `COUNT`.

**Đánh đổi:** Cursor không ký nên có thể bị sửa; full page bỏ qua `after`, partial route 400 nếu
thiếu — hai contract cho cùng query param (README ghi nhận).

Nguồn: `internal/usecase/folder/folder.go`, `internal/entity/folder.go`,
`internal/repo/persistent/{folder,file}`.

### 11.4. Cây thư mục: depth, cycle, rename/move/duplicate

**Vấn đề:** Cây tự tham chiếu dễ tạo cycle, move vào chính subtree, vượt depth 20; duplicate cả
subtree phải copy hàng loạt object.

**Giải pháp:**

- `FolderDepth`, `FolderSubtreeHeight` tính từ danh sách folder (`internal/entity/folder.go`).
- Move/duplicate kiểm tra `destination` không nằm trong subtree nguồn, depth sau move ≤ 20.
- Khóa user/root + subtree trong transaction.
- Duplicate tạo row mới cho từng folder/file, copy object bằng `CopyObject`, rollback/cleanup nếu
  lỗi.
- Form move/duplicate pre-filter destination bằng cùng công thức để UI và use case khớp.

**Lý do:** DB không có constraint chống cycle; app phải là nơi enforce. Pre-filter giảm lỗi người
dùng.

**Đánh đổi:** `Create` đọc toàn bộ cây để tính depth; duplicate giữ lock qua N lần `CopyObject`
(README ghi nhận).

Nguồn: `internal/usecase/folder/folder.go`.

### 11.5. Session role snapshot & thu hồi

**Vấn đề:** Guard cần biết role mỗi request nhưng không muốn query Postgres. Nếu cache role trong
session, đổi role phải làm gì với session cũ?

**Giải pháp:**

- Session Redis chứa `{user_id, role}`.
- Đổi role → `SetRole` cập nhật DB rồi `DeleteUserSessions`.
- Đổi password → giữ session hiện tại, xóa các session khác (`DeleteUserSessionsExcept`).
- Disable → xóa tất cả session.
- Redis giữ index `user-sessions:<user_id>` để xóa hàng loạt.

**Lý do:** Guard chỉ đọc Redis, giảm tải DB; hành vi bảo mật rõ ràng.

**Đánh đổi:** Role là snapshot; user phải đăng nhập lại sau khi đổi role. Ghi DB trước khi xóa
Redis có thể để lại session cũ nếu Redis fail (README ghi nhận).

Nguồn: `internal/usecase/user/user.go`, `internal/repo/session/session.go`,
`internal/controller/web/middleware/guard.go`.

### 11.6. Login throttling hai bucket

**Vấn đề:** Chống brute-force một account và chống password spray từ một IP, nhưng không được
khóa nhầm người dùng hợp lệ.

**Giải pháp:**

- Email bucket: 5 lần/15 phút.
- IP bucket: 30 lần/15 phút.
- Mỗi attempt reserve cả hai trước khi check password; atomic Lua.
- Login thành công reset email bucket, không reset IP bucket.

**Lý do:** Hai bucket bảo vệ hai kiểu tấn công khác nhau; IP bucket không reset để một account
hợp lệ không mở khóa budget của kẻ tấn công cùng IP.

**Đánh đổi:** NAT chung có thể khóa 31 người; email bucket bị charge trước IP bucket nên kẻ xấu có
thể đốt slot của victim (README ghi nhận).

Nguồn: `internal/usecase/user/user.go`, `internal/repo/transfer/login.go`.

### 11.7. CSRF cho htmx + XHR

**Vấn đề:** htmx gửi request bằng fetch/XHR; cookie tự gửi nhưng header phải tự thêm. Upload
driver dùng XHR riêng, và `navigator.sendBeacon` không set được header.

**Giải pháp:**

- Double-submit cookie: cookie `csrf_token` (không HttpOnly) + header `X-CSRF-Token`.
- Listener `htmx:config:request` tự gắn header cho mọi request htmx (`web/static/js/app.js`).
- Upload driver tự lấy cookie và gắn header cho `fetch`/XHR.
- Abort dùng `fetch(..., { keepalive: true })` thay vì beacon.
- So sánh constant-time; kiểm tra Origin.

**Lý do:** Double-submit không cần server-side state; htmx listener đảm bảo không quên header.

**Đánh đổi:** Token không rotate; cookie đọc được bằng JS (necessary); Origin check bỏ qua scheme.

Nguồn: `internal/controller/web/middleware/csrf.go`, `web/static/js/app.js`.

### 11.8. Nhất quán giữa Postgres và object storage

**Vấn đề:** Transaction Postgres không bao trùm MinIO. Nếu ghi row fail sau khi object đã tạo, cần
dọn object; nếu xóa row trước khi xóa object, object mồ côi.

**Giải pháp:**

- Complete: join object trước, ghi row trong transaction; nếu row fail → xóa object vừa join.
- Duplicate: copy object, ghi row; nếu row fail → xóa bản copy.
- Delete: xóa row trước, lấy storage key, xóa object sau; object mồ côi vô hình, chấp nhận.
- Cleanup fail được join vào error với sentinel `ErrStorageCleanup`.

**Lý do:** Ưu tiên trạng thái "row luôn trỏ tới object tồn tại"; object mồ côi có thể dọn bằng job
sau này (chưa có).

**Đánh đổi:** Không có reconciliation job; `storage_used_bytes` có thể drift.

Nguồn: `internal/usecase/file/upload.go` (`finalizeError`, `joinError`),
`internal/usecase/file/file.go` (`finalizeDuplicateError`), `internal/usecase/folder/folder.go`
(`removeAll`).

### 11.9. Observability không làm chậm app

**Vấn đề:** Cần request ID, metrics, pprof nhưng không muốn ảnh hưởng request path hoặc expose
diagnostics ra public.

**Giải pháp:**

- Middleware đọc 2 clock, tăng counter; label là route template.
- Request ID sinh bằng UUID, chỉ nhận inbound an toàn.
- Metrics + pprof trên listener `net/http` riêng, bind loopback mặc định.
- Tắt listener khi prefork hoặc `OBSERVE_ENABLED=false`.
- Access log bỏ qua `/healthz`, `/static/`.

**Lý do:** Tách listener tránh CSRF/body limit/fake latency và tránh expose pprof qua Fiber.

**Đánh đổi:** Listener riêng không đi qua auth; phải bảo vệ bằng network binding. Prefork không có
metrics.

Nguồn: `internal/controller/web/middleware/{metrics,requestid,logger}.go`,
`internal/app/observe.go`, `pkg/metrics/metrics.go`.

### 11.10. Test suite "fail vì đúng lý do"

**Vấn đề:** Test cũ assert SQL string/HTML class, pass dù app hỏng; coverage không đều.

**Giải pháp:**

- Chuyển sang test hành vi: quyền sở hữu, status code, state đã lưu.
- Fake repo assert đúng `userID`/`folderID` được truyền.
- Integration test thật với Postgres + MinIO sau build tag.
- Fuzz cursor decoder.
- CI chạy race + shuffle.

**Lý do:** Test phải bắt regression thật, không phải khóa implementation detail.

**Đánh đổi:** Coverage tổng không cao (39.4%) vì repo layer chỉ test qua integration.

Nguồn: `docs/superpowers/specs/2026-11-22-meaningful-test-suite-design.md`, `*_test.go`.

---

## 12. Số liệu định lượng từ codebase

Đo tại 2026-11-25 trên working tree hiện tại.

| Chỉ số                                               | Giá trị                                                                          | Cách đo / nguồn                                 |
| ---------------------------------------------------- | -------------------------------------------------------------------------------- | ----------------------------------------------- |
| Tổng commit                                          | **127**                                                                          | `git rev-list --count HEAD`                     |
| Tác giả                                              | **1** (`minhhoccode111`)                                                         | `git shortlog -sne --all`                       |
| Commit đầu tiên                                      | 2026-07-11                                                                       | `git log --reverse`                             |
| Giai đoạn phát triển chính                           | 2026-10-14 → 2026-11-25 (**43 ngày**)                                            | `git log --date=short`                          |
| Commit trong giai đoạn chính                         | **126**                                                                          | 127 trừ initial commit                          |
| Merge commit                                         | 1                                                                                | `git log --merges`                              |
| Lượt file thay đổi (file touches)                    | **1.354**                                                                        | `git log --shortstat`                           |
| Dòng thêm / xóa                                      | **96.754 / 27.463**                                                              | `git log --shortstat`                           |
| Phân loại commit                                     | 63 feat, 21 fix, 15 test, 10 docs, 9 chore, 5 refactor, 2 ci, 1 initial, 1 merge | `git log --format=%s`                           |
| File Go                                              | **163**                                                                          | `find . -name '*.go'`                           |
| Package Go                                           | **32**                                                                           | unique directory chứa `.go`                     |
| LOC Go production (không test/generated/integration) | **15.484**                                                                       | `wc -l`                                         |
| LOC Go test                                          | **19.669**                                                                       | `find -name '*_test.go'`                        |
| LOC templ                                            | **2.712**                                                                        | `wc -l web/templates/*.templ`                   |
| Component templ                                      | **76**                                                                           | `grep '^templ '`                                |
| File `.templ`                                        | **10**                                                                           | `ls web/templates/*.templ`                      |
| LOC Go generated từ templ                            | **6.374**                                                                        | `*_templ.go`                                    |
| LOC JavaScript tự viết                               | **655**                                                                          | `wc -l web/static/js/app.js`                    |
| LOC SQL migration                                    | **129**                                                                          | `wc -l migrations/*.sql`                        |
| Số migration                                         | **7 cặp** (14 file up/down)                                                      | `migrations/`                                   |
| Bảng DB nghiệp vụ                                    | **4** (`users`, `folders`, `files`, `shares`)                                    | `docs/schema.sql`                               |
| Bảng metadata                                        | 1 (`schema_migrations`)                                                          | `docs/schema.sql`                               |
| Index/unique constraint                              | **12 index + 3 unique constraint**                                               | `docs/schema.sql`                               |
| Route đăng ký                                        | **61** (37 GET, 23 POST, 1 PUT)                                                  | `internal/controller/web/router.go`             |
| Handler method trên `Controller`                     | ~101 method (gồm helper)                                                         | `grep 'func (c *Controller)'`                   |
| Use case method                                      | **51** (User 16, Folder 13, Share 7, File 15)                                    | `internal/usecase/contracts.go`                 |
| Repo interface                                       | **10**                                                                           | `internal/repo/contracts.go`                    |
| Middleware                                           | **9** nhóm chức năng                                                             | `middleware/`                                   |
| Package `pkg`                                        | **8**                                                                            | `pkg/`                                          |
| Test function                                        | **470 `Test*` + 1 `Fuzz*`**                                                      | `grep -c '^func Test'` + `grep -c '^func Fuzz'` |
| File test                                            | **64**                                                                           | `find -name '*_test.go'`                        |
| Integration test file                                | **7**                                                                            | `internal/repo/persistent/integration/`         |
| Coverage statement (unit)                            | **39.4%**                                                                        | `go test -coverprofile` + `go tool cover -func` |
| Linter bật                                           | **47** + 4 formatter                                                             | `.golangci.yml`                                 |
| Make target                                          | **37**                                                                           | `grep '^[a-zA-Z0-9_-]*:' Makefile`              |
| Docker service                                       | **5**                                                                            | `docker-compose.yml`                            |
| CI job                                               | **7**                                                                            | `.github/workflows/ci.yml`                      |
| README                                               | 589 dòng                                                                         | `wc -l README.md`                               |
| AGENTS.md                                            | ~30 KB                                                                           | `ls -l AGENTS.md`                               |

**Số liệu thực tế do người dùng xác nhận (không đo từ code):**

| Chỉ số                      | Giá trị                                                        | Ghi chú                                                                                                |
| --------------------------- | -------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| Môi trường deploy           | **Đã deploy 2026-11-26**                                       | VPS cá nhân, Docker Compose (`deploy/`), Caddy host-native systemd, CI/CD GitHub Actions chạy thật    |
| Registry                    | **Docker Hub `minhhoccode111/fileuploader`**                  | Tag `latest` + commit sha mỗi lần push main; mỗi bootstrap push tay một lần đầu                       |
| App URL                     | **`https://fileuploader.minhhoccode111.com`**                 | Đang chạy; `/healthz` 200                                                                              |
| TLS                         | **Wildcard `*.minhhoccode111.com` qua Cloudflare DNS-01**     | Caddy host-native systemd (repo `devops/caddy`), snippet riêng cho app                                  |
| Vai trò                     | **Dev chính**                                                  | Team size 1; project cá nhân để học                                                                    |
| AI hỗ trợ                   | **Phần lớn code**                                              | Cần cân nhắc cách mô tả trung thực khi viết CV                                                         |
| Review process              | **AI review → open-code-review (ocr) → bạn đọc review cuối**   | Không có human reviewer khác (theo mô tả)                                                              |
| File lớn nhất đã test local | **6 GB**                                                       | Upload 6 GB bị chặn đúng bởi quota demo 5 GiB trên prod                                                |
| Đo hiệu năng                | **Chưa đo**                                                    | Không có throughput, p95 latency, request/s                                                            |
| Số người dùng thật          | **Chưa có**                                                    | Mới mở account admin + demo tự seed                                                                     |

**Lưu ý về số liệu:**

- 127 commit là trên nhánh `develop` (`HEAD`); `git log --all` thấy thêm 2 stash entry nên
  `git shortlog --all` báo 129.
- Churn 96.754 dòng thêm bao gồm generated `*_templ.go`, vendor JS, `app.css` và `docs/schema.sql`;
  không phải toàn bộ là code tay.
- Coverage 39.4% là của `go test ./...` (không gồm integration). Nếu chạy integration, coverage
  repo layer tăng đáng kể nhưng không có số đo tổng hợp trong repo.
- Số route 61 tính cả `/healthz`; `/static` không tính.
- Số handler ~101 là tất cả method trên `*Controller`, gồm helper/render; không phải 101 endpoint.
- CI hiện có 7 job chất lượng chạy trên push `main`, **cộng thêm workflow deploy tự động**
  (`deploy.yml`: test → build/push Docker Hub → ssh deploy); xem mục 10.3.
- Chưa có số liệu người dùng, dung lượng thực tế, hiệu năng hay load test; mọi con số trong bảng
  chính đều đo từ code/git, không phải từ production.

---

## 13. Đóng góp & vai trò

### 13.1. Dữ liệu từ git log

- **Một tác giả duy nhất:** `minhhoccode111 <minhhoccode111@gmail.com>`; 127 commit.
- Không có evidence của contributor khác trong git history. AI hỗ trợ code không để lại dấu trong
  commit, nên git không phản ánh mức độ AI tham gia.
- Commit message theo Conventional Commits (`feat`, `fix`, `test`, `docs`, `chore`, `refactor`,
  `ci`), cho thấy quy trình commit có kỷ luật.
- Các cụm commit lớn theo thời gian:
  - 2026-10-14 → 2026-10-15: strip template, thêm templ/Tailwind/MinIO, chuyển từ REST sang web
    MVC, htmx fragment.
  - 2026-11-02: user limits + storage quota.
  - 2026-11-14: chunked upload.
  - 2026-11-19 → 2026-11-20: user status, access requests.
  - 2026-11-21: folder browse + search (keyset, pg_trgm).
  - 2026-11-22: test suite "meaningful".
  - 2026-11-24: observability (request ID, metrics, pprof).
  - 2026-11-25: docs, learned concepts, rough edges.

### 13.2. Những mảng code thể hiện trong repo

Nếu bạn là tác giả duy nhất, bạn đã chạm vào toàn bộ các mảng sau (có file tương ứng):

| Mảng                                             | Bằng chứng                                                           |
| ------------------------------------------------ | -------------------------------------------------------------------- |
| Thiết kế domain & sentinel error                 | `internal/entity/*.go`                                               |
| Thiết kế schema + migration up/down              | `migrations/` 7 cặp                                                  |
| Repository Postgres (Squirrel, pgx, transaction) | `internal/repo/persistent/`                                          |
| Redis session/upload/rate/limit                  | `internal/repo/{session,upload,transfer}/`                           |
| Use case & business rule                         | `internal/usecase/`                                                  |
| Fiber handler + middleware + DTO                 | `internal/controller/web/`                                           |
| templ + htmx + Alpine UI                         | `web/templates/`, `web/static/js/app.js`                             |
| Upload protocol end-to-end                       | `usecase/file/upload.go`, `controller/web/upload.go`, `app.js`       |
| Observability                                    | `pkg/metrics/`, `middleware/`, `app/observe.go`                      |
| Docker/Compose/nginx/Makefile/CI                 | `Dockerfile`, `docker-compose.yml`, `nginx/`, `Makefile`, `.github/` |
| Test suite + fuzz + integration                  | `*_test.go`, `internal/repo/persistent/integration/`                 |
| Tài liệu kỹ thuật                                | `README.md`, `AGENTS.md`, `docs/superpowers/`                        |

### 13.3. Vai trò & bối cảnh

- **Team size 1**, bạn là **dev chính**; đây là project cá nhân để học, không phải sản phẩm thương
  mại (`README.md`).
- **AI hỗ trợ phần lớn code** (người dùng xác nhận). Vì vậy khi viết CV, nên mô tả trung thực vai
  trò của bạn là: thiết kế kiến trúc/schema, đặt yêu cầu, review, tích hợp, debug, test và vận hành
  — thay vì nhận "tự viết 100% code".
- **Thời gian effort thực tế:** chưa xác định. Git cho thấy 43 ngày lịch (2026-10-14 → 2026-11-25),
  nhưng đây không phải số giờ làm việc.
- **Quy trình review:** AI review trước → một vòng **open-code-review (ocr)** → bạn đọc review cuối
  và quyết định sửa. Theo mô tả, không có mentor/reviewer con người khác.
- **Chưa deploy:** mới test local, file lớn nhất 6 GB; chưa có số liệu người dùng/hiệu năng.

---

## 14. Điểm nổi bật tiềm năng cho CV

Sắp xếp theo mức độ ấn tượng với nhà tuyển dụng. Chỉ nêu điểm có thật trong code hoặc đã được người
dùng xác nhận; phần "kế hoạch" được ghi rõ là chưa làm.

### 14.1. Thiết kế và triển khai chunked upload protocol đầy đủ

- 4 endpoint: start/part/complete/abort; JSON part plan; ETag; idempotent complete; quota
  reservation; sweeper dọn session/multipart.
- Client XHR gửi tuần tự, retry exponential backoff + jitter, abort keepalive.
- Đã test local với file **6 GB**; chưa đo throughput.
- Bằng chứng: `internal/usecase/file/upload.go`, `internal/controller/web/upload.go`,
  `internal/repo/upload/upload.go`, `internal/app/sweeper.go`, `web/static/js/app.js`.

**CV bullet gợi ý:** "Thiết kế chunked upload 10 GiB với S3 multipart, quota reservation theo tài
khoản, complete idempotent và background sweeper dọn part dở dang."

### 14.2. Rate limiter phân tán bằng Redis Lua

- Một bucket/account/direction; virtual next-send pointer; atomic Lua; dùng chung cho upload,
  download file, download zip, share download.
- Bằng chứng: `internal/repo/transfer/transfer.go`, `internal/usecase/pace/reader.go`.

**CV bullet gợi ý:** "Xây per-account rate limiter bằng Redis Lua, chia sẻ budget giữa nhiều stream
và nhiều instance mà không cần coordinator."

### 14.3. Keyset pagination + trigram search

- 100 entry/page; folders-first two-pass cursor; composite index `(user_id, lower(name), id)`;
  `pg_trgm` GIN cho search toàn drive; cursor fuzz-tested.
- Bằng chứng: `internal/usecase/folder/folder.go`, `internal/entity/folder.go`,
  `migrations/20261121044558_add_folder_browse_indexes.up.sql`, `internal/entity/fuzz_test.go`.

**CV bullet gợi ý:** "Triển khai keyset pagination cho listing folder trộn folder/file, kèm global
search dùng pg_trgm và cursor fuzz-tested."

### 14.4. Kiến trúc layered/clean với Unit of Work

- Controller → use case → repo interface; composition root duy nhất; transaction propagation qua
  context; S3 compensation.
- Bằng chứng: `internal/app/app.go`, `internal/{usecase,repo}/contracts.go`,
  `pkg/postgres/tx.go`.

**CV bullet gợi ý:** "Thiết kế layered architecture với interface ở mỗi tầng, Unit of Work cho
transaction boundary và compensation cho object storage."

### 14.5. Session & role invalidation

- Redis session `{user_id, role}`; index session theo user; đổi role/password/disable thu hồi
  đúng tập session.
- Bằng chứng: `internal/repo/session/session.go`, `internal/usecase/user/user.go`,
  `middleware/guard.go`.

**CV bullet gợi ý:** "Thiết kế session store Redis với role snapshot và cơ chế thu hồi session
theo role/password/status."

### 14.6. Bảo mật thực dụng

- CSRF double-submit + Origin check; login throttle 2 bucket; bcrypt; tenant scoping; share
  subtree; body limit; request ID an toàn.
- Bằng chứng: `middleware/{csrf,bodylimit,requestid,guard}.go`, `usecase/user/user.go`,
  `usecase/share/share.go`.

**CV bullet gợi ý:** "Áp dụng CSRF double-submit, login throttling hai bucket, bcrypt và
tenant-scoped query cho ứng dụng multi-tenant."

### 14.7. Observability không xâm nhập

- Request ID xuyên log; Prometheus metrics với route template label; pprof listener riêng loopback;
  tắt khi prefork.
- Bằng chứng: `pkg/metrics/metrics.go`, `middleware/{metrics,requestid,logger}.go`,
  `internal/app/observe.go`.

**CV bullet gợi ý:** "Thêm request ID, Prometheus metrics và pprof trên private listener, tránh
cardinality nổ và không expose diagnostics ra public."

### 14.8. Test suite có chiều sâu

- 470 test functions + fuzz; fake repo; miniredis; integration Postgres/MinIO; race + shuffle
  trong CI.
- Bằng chứng: `*_test.go`, `internal/repo/persistent/integration/`, `.github/workflows/ci.yml`.

**CV bullet gợi ý:** "Xây test suite 470 test function gồm unit, HTTP, Redis (miniredis), fuzz và
integration Postgres/MinIO, chạy race + shuffle trong CI."

### 14.9. Streaming download & ZIP

- Download stream qua app để áp rate limit; ZIP stream bằng `io.Pipe`; `WriteTimeout=0`;
  `Content-Disposition` RFC 5987.
- Bằng chứng: `internal/usecase/folder/folder.go`, `internal/controller/web/{file,download}.go`,
  `pkg/httpserver/server.go`.

**CV bullet gợi ý:** "Stream file và ZIP qua app để áp per-account rate limit mà không buffer toàn
bộ nội dung trong RAM."

### 14.10. Server-rendered UI không SPA

- templ + htmx 4 + Alpine 3 + Tailwind 4 + daisyUI 5; partial swap; drawer; progress; skeleton;
  `HX-Trigger`/`HX-Push-Url`.
- Bằng chứng: `web/templates/`, `web/static/js/app.js`, `web/app.css`.

**CV bullet gợi ý:** "Xây UI server-rendered bằng templ + htmx + Alpine, không cần Node/SPA, vẫn có
progress bar, drawer và partial update."

### 14.11. DevOps & chất lượng

- Docker multi-stage scratch; Compose 5 service; nginx; Makefile 37 target; CI 7 job; 47 linter.
- **Deploy tự động đã chạy thật (2026-11-26):** push `main` → GitHub Actions test → build/push
  Docker Hub (`latest` + sha, cache GHA) → SSH vào VPS (key pin `restrict,command=` chỉ chạy được
  script deploy) → `docker compose pull && up -d`. Caddy host-native systemd với wildcard TLS
  `*.minhhoccode111.com` (Cloudflare DNS-01) làm front. Đây là bullet self-hosting thật.
- Bằng chứng: `Dockerfile`, `deploy/` (compose prod + script), `.github/workflows/{ci,deploy}.yml`,
  repo `devops/caddy`.

**CV bullet gợi ý:** "Đóng gói multi-stage Docker image từ scratch, dựng Compose stack 5 service và
CI 7 job gồm lint, test race/shuffle, integration."

### 14.12. Tài liệu kỹ thuật & quy trình

- README 589 dòng với bài học và known rough edges; AGENTS.md quy ước; 5 design spec + 5 plan.
- Bằng chứng: `README.md`, `AGENTS.md`, `docs/superpowers/`.

**CV bullet gợi ý:** "Viết tài liệu kiến trúc, convention và design spec/plan cho từng feature lớn;
ghi lại known rough edges thay vì che giấu."

> **Lưu ý:** Dự án **đã deploy** (2026-11-26) nhưng **chưa đo hiệu năng**, nên chưa nên viết các
> bullet kiểu "phục vụ N người dùng" hay "giảm p95 X%". Bullet self-hosting + CI/CD tự động giờ là
> sự thật có thể ghi thẳng.

---

## 15. Từ khóa kỹ thuật (ATS keywords)

### Languages

- Go (Golang) 1.27
- JavaScript (vanilla, XHR, Fetch API)
- SQL (PostgreSQL)
- HTML5 / CSS3
- Lua (Redis script)
- Bash (Makefile, script)

### Frameworks & Libraries

- Fiber v2 (fasthttp)
- templ
- htmx 4
- Alpine.js 3
- Tailwind CSS 4
- daisyUI 5
- go-playground/validator v10
- Squirrel (SQL builder)
- pgx v5
- go-redis v9
- minio-go v7
- zerolog
- golang-migrate
- caarlos0/env
- Prometheus client_golang
- bcrypt (`golang.org/x/crypto`)
- `golang.org/x/sync/errgroup`
- google/uuid

### Databases & Storage

- PostgreSQL 18
- Redis 8
- MinIO / S3-compatible object storage
- S3 Multipart Upload
- Redis Lua scripting
- Redis TTL / sorted set / set / hash
- PostgreSQL recursive CTE
- PostgreSQL GIN index
- `pg_trgm`
- Keyset pagination
- Unique partial index
- CHECK constraint
- Foreign key cascade
- Transaction / Unit of Work

### Cloud & DevOps

- Docker
- Docker Compose
- Multi-stage build
- `scratch` image
- nginx reverse proxy
- Caddy reverse proxy
- Caddy host-native systemd
- systemd service / EnvironmentFile
- Cloudflare proxied DNS
- Cloudflare DNS-01 challenge / wildcard TLS
- VPS hosting
- Self-hosting / single-VPS deployment
- Docker Hub (`minhhoccode111/fileuploader`)
- GitHub Actions
- CI/CD (lint, test, integration, build/push image, ssh deploy)
- Prometheus
- pprof
- Health check (`/healthz`)
- Environment-based configuration
- Graceful shutdown
- Prefork mode

### Tools

- Make
- golangci-lint
- gofumpt
- gci
- goimports
- govulncheck
- Atlas
- air (hot reload)
- miniredis
- Git
- Conventional Commits
- open-code-review (ocr)
- AI-assisted code review
- hadolint
- yamllint
- dotenv-linter
- Nancy

### Concepts & Patterns

- Layered architecture
- Clean architecture / Hexagonal (ports & adapters)
- Monolith
- Server-side rendering (SSR)
- MVC-ish transport
- Dependency injection via constructor
- Composition root
- Repository pattern
- Unit of Work
- Sentinel errors
- Error wrapping (`%w`, `errors.Is`, `errors.Join`)
- Multi-tenancy / tenant isolation
- RBAC (admin/user)
- Session-based authentication
- CSRF double-submit cookie
- Rate limiting (token bucket-ish virtual scheduling)
- Quota management
- Chunked/multipart upload
- Idempotency
- Background worker / sweeper
- Streaming I/O
- Cursor pagination
- Full-text/trigram search
- Observability (logs, metrics, profiling)
- Request ID / correlation ID
- Structured logging
- Graceful degradation
- Fuzz testing
- Integration testing
- Race detector
- Test shuffle
- Table-driven tests
- Fake repositories
- Feature-based commit history
- Design docs / ADR-like specs

---

## 16. Hạn chế & hướng cải thiện

Phần này nói thẳng để chuẩn bị phỏng vấn. Nhiều điểm đã được tác giả ghi trong README mục
_Known rough edges_; một số do quá trình khảo sát phát hiện thêm.

### 16.1. Bảo mật

| Vấn đề                                | Ảnh hưởng                                                                                                                                           | Hướng cải thiện                                                                                               | Nguồn                                           |
| ------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- | ----------------------------------------------- |
| Disable account không chặn share link | Tài khoản bị khóa vẫn phục vụ public share                                                                                                          | Resolve share phải đọc status owner, hoặc xóa share khi disable                                               | `README.md`                                     |
| Không có last-admin guard             | Admin cuối có thể tự demote/disable, mất quyền quản trị                                                                                             | Kiểm tra số admin active trước khi đổi role/status                                                            | `README.md`, `internal/controller/web/admin.go` |
| Registration không throttle           | Bcrypt CPU sink, spam account pending                                                                                                               | Kế hoạch demo: chỉ tạo sẵn một account demo quota giới hạn, credential hiện ở login; vẫn nên throttle/CAPTCHA | `README.md`                                     |
| Enumeration oracle                    | 409 email tồn tại; 422 password sai                                                                                                                 | Trả message chung, thêm delay đồng nhất                                                                       | `README.md`                                     |
| Timing side channel                   | Unknown email trả trước bcrypt                                                                                                                      | Luôn chạy bcrypt với dummy hash                                                                               | `README.md`                                     |
| Cookie `Secure` sau TLS               | nginx bundled không set `X-Forwarded-Proto`; Caddy + Cloudflare **Full (Strict)** thì app thấy `https`, nhưng cookie vẫn phụ thuộc `ctx.Protocol()` | Kiểm tra cookie sau deploy; nginx local vẫn có vấn đề                                                         | `README.md`, `nginx/nginx.conf`, `devops/caddy` |
| `X-Real-IP` tin cậy                   | Snippet Caddy set `X-Real-IP {remote_host}`, nhưng Cloudflare proxied làm `remote_host` = IP Cloudflare; thiếu header thì mọi khách chung IP bucket | Trust Cloudflare ranges + `{client_ip}`/`CF-Connecting-IP`; kiểm tra snippet app                              | `README.md`, repo `devops/caddy`                |
| CSRF token không rotate               | Token sống lâu                                                                                                                                      | Rotate sau login/logout                                                                                       | `README.md`                                     |
| Share token = UUID row                | Không có scope/rotation                                                                                                                             | Sinh token riêng, hash trong DB, cho phép revoke/rotate                                                       | `internal/entity/share.go`                      |
| Không có audit log                    | Không truy vết thao tác admin                                                                                                                       | Thêm bảng audit + log                                                                                         | —                                               |
| Không có global HTTP rate limit       | Flood request thường không bị chặn; chỉ login và upload start có giới hạn                                                                           | Thêm limiter theo IP/route                                                                                    | `internal/controller/web/router.go`             |

### 16.2. Nhất quán dữ liệu

| Vấn đề                                                       | Ảnh hưởng                                          | Hướng cải thiện                                          | Nguồn                                           |
| ------------------------------------------------------------ | -------------------------------------------------- | -------------------------------------------------------- | ----------------------------------------------- |
| Admin save là 3 write độc lập                                | Update user, limits, status có thể fail giữa chừng | Gộp vào một use case + Unit of Work                      | `internal/controller/web/admin.go`              |
| `SetRole`/`UpdateUser` ghi DB trước, xóa Redis sau           | Redis fail → session cũ sống                       | Dùng outbox/retry; hoặc ghi Redis trước với compensation | `README.md`                                     |
| `DeleteUserSessionsExcept` đọc set rồi xóa ngoài transaction | Concurrent login có thể lọt                        | Lua script atomic                                        | `internal/repo/session/session.go`              |
| Storage cleanup fail chỉ log                                 | Object mồ côi, quota drift                         | Job reconciliation định kỳ                               | `README.md`                                     |
| `storage_used_bytes` app-maintained                          | Có thể lệch                                        | Trigger hoặc job đối soát                                | `README.md`                                     |
| Approve tài khoản không `pending` trả 500                    | `ErrInvalidStatus` chưa có mapping admin           | Map sang 422/409                                         | `README.md`, `internal/controller/web/admin.go` |
| Cross-table name uniqueness chỉ ở app                        | Folder/file trùng tên có thể lọt khi race          | DB constraint phức tạp hoặc serializable isolation       | `README.md`                                     |
| Cycle chỉ app prevent                                        | DB không có `CHECK (parent_id <> id)`              | Thêm constraint + cycle clause trong CTE                 | `README.md`                                     |
| Stale multipart sweep dùng S3 ID tra Redis key               | Live upload > 24h có thể bị abort nhầm             | Lưu mapping S3 multipart ID → session ID                 | `README.md`                                     |
| Sweep dừng ở batch không xóa được                            | Session due bị trễ                                 | Tiếp tục batch, chỉ dừng khi hết                         | `README.md`                                     |

### 16.3. Chất lượng code / kiến trúc

| Vấn đề                                                                     | Ảnh hưởng                                                                     | Hướng cải thiện                                | Nguồn                                                                   |
| -------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | ---------------------------------------------- | ----------------------------------------------------------------------- |
| Constructor variadic `...any`                                              | Mất type-safety, khó đọc                                                      | Dùng struct config hoặc constructor tường minh | `internal/usecase/{folder,file,share}/*.go`                             |
| Nhiều method alias trong `usecase.File`/`Folder`                           | Interface phình, khó hiểu                                                     | Giữ một tên chuẩn, bỏ alias                    | `internal/usecase/contracts.go`                                         |
| Hằng số 255/20 lặp nhiều package                                           | Dễ lệch                                                                       | Tập trung vào entity constant                  | `README.md`                                                             |
| Cursor không ký                                                            | Client sửa được, nhảy trang                                                   | HMAC hoặc opaque server-side token             | `README.md`                                                             |
| Full page bỏ qua `after`, partial 400                                      | Hai contract cho cùng param                                                   | Thống nhất behavior                            | `README.md`                                                             |
| `Create` đọc toàn cây để tính depth                                        | Chậm với drive lớn                                                            | Materialized path / closure table              | `internal/usecase/folder/folder.go`                                     |
| Duplicate giữ lock qua N `CopyObject`                                      | Transaction dài, block account                                                | Copy ngoài transaction + compensation          | `README.md`                                                             |
| Row lock trên root folder                                                  | Serialize mọi write của một account                                           | Lock theo folder/subtree                       | `internal/repo/persistent/folder/folder.go`                             |
| `docs/schema.sql` ghi `status DEFAULT 'active'`, migration ghi `'pending'` | Default đúng là `pending` (app set explicit + migration); schema dump bị lệch | Regenerate `docs/schema.sql` từ DB sạch        | `docs/schema.sql` vs `migrations/20261119155114_add_user_status.up.sql` |
| `File.Download` (presigned) không còn route gọi                            | Dead code, dễ gây nhầm                                                        | Xóa hoặc document rõ                           | `internal/usecase/file/file.go`, `internal/usecase/contracts.go`        |

### 16.4. Vận hành

| Vấn đề                                     | Ảnh hưởng                                                  | Hướng cải thiện                                                                                                                                                                             | Nguồn                                                       |
| ------------------------------------------ | ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------- |
| Chưa có backup volume + monitor disk       | Rủi ro mất dữ liệu/đầy disk khi demo                       | Backup Postgres/MinIO volume, alert disk (mục còn mở sau deploy)                                                                                                                            | `deploy/docker-compose.prod.yml`                           |
| Không có tracing                           | Khó debug cross-service (dù chỉ 1 service)                 | OTel khi có service thứ hai                                                                                                                                                                 | `docs/superpowers/specs/2026-11-24-observability-design.md` |
| Không có alert/dashboard                   | Metrics chỉ để xem                                         | Grafana + alert rules                                                                                                                                                                       | —                                                           |
| Không có backup strategy trong repo        | Rủi ro mất dữ liệu                                         | Document volume backup                                                                                                                                                                      | `docker-compose.yml`                                        |
| `make docker-rm-volume` sai tên volume     | Lệnh không xóa DB                                          | Sửa thành `fileuploader_db_data`                                                                                                                                                            | `Makefile`, `README.md`                                     |
| Migration tự chạy khi start                | Không kiểm soát rollout                                    | Tách job migrate                                                                                                                                                                            | `internal/app/migrate.go`                                   |
| Prefork tắt metrics                        | Mất observability khi scale                                | Dùng sidecar/port riêng                                                                                                                                                                     | `internal/app/observe.go`                                   |
| Secret chỉ ở env                           | Không có vault/rotation                                    | Dùng secret manager khi deploy                                                                                                                                                              | `config/config.go`                                          |
| Production config còn là dev default       | `LOG_LEVEL=debug`, fake latency 100–200ms, secret mặc định | Đặt `LOG_LEVEL=info`, `HTTP_FAKE_LATENCY_MAX=0`, secret riêng                                                                                                                               | `docker-compose.yml`, `.env.example`                        |
| VPS một node, chưa có backup/giám sát disk | Rủi ro mất dữ liệu/đầy disk khi demo                       | Backup volume, quota demo, monitor disk                                                                                                                                                     | `docker-compose.yml`                                        |

### 16.5. UX / frontend

| Vấn đề                                     | Ảnh hưởng                  | Hướng cải thiện                         | Nguồn       |
| ------------------------------------------ | -------------------------- | --------------------------------------- | ----------- |
| Row dùng chuột, không keyboard             | Accessibility kém          | Thêm tabindex/role/key handler          | `README.md` |
| Admin submenu vẫn trong tab order khi đóng | Keyboard focus vào link ẩn | `inert`/`hidden`                        | `README.md` |
| Upload không resume sau reload             | Upload 10 GiB phải làm lại | Lưu upload ID vào localStorage/server   | `README.md` |
| Part gửi tuần tự                           | Chậm với file lớn          | Parallel parts có giới hạn              | `README.md` |
| Progress rewind khi retry                  | UX gây nhầm                | Tính progress theo offset đã hoàn thành | `README.md` |
| Complete fail sau 99% phải upload lại      | Tốn băng thông             | Retry complete; giữ session             | `README.md` |

### 16.6. Test

| Vấn đề                | Ảnh hưởng                                                      | Hướng cải thiện                            | Nguồn                                                               |
| --------------------- | -------------------------------------------------------------- | ------------------------------------------ | ------------------------------------------------------------------- |
| Coverage 39.4% (unit) | Repo layer ít được phủ                                         | Chạy integration trong CI + coverage merge | đo tại 2026-11-25                                                   |
| Không có browser E2E  | Upload UI/htmx chưa được test tự động                          | Playwright/chromedp                        | `docs/superpowers/specs/2026-11-22-meaningful-test-suite-design.md` |
| Chưa đo hiệu năng     | Chỉ biết upload 6 GB local thành công; không có throughput/p95 | Benchmark upload/download; đo trên VPS     | người dùng xác nhận                                                 |
| Không có load test    | Chưa biết throughput                                           | k6/Locust                                  | —                                                                   |

---

## 17. Câu hỏi cần bổ sung

Các mục 17.1–17.5 là phần còn thiếu; 17.0 ghi lại những gì đã xác nhận để tránh hỏi lại.

### 17.0. Đã xác nhận (cập nhật 2026-11-26)

- Team size **1**; bạn là **dev chính**; đây là project cá nhân để học.
- **AI hỗ trợ phần lớn code**; review theo thứ tự: **AI review → open-code-review (ocr) → bạn đọc
  review cuối**.
- **Đã deploy 2026-11-26** tại `https://fileuploader.minhhoccode111.com`: VPS cá nhân, app chạy
  Docker Compose (`deploy/`), image trên **Docker Hub `minhhoccode111/fileuploader`**, Caddy
  **host-native systemd** làm reverse proxy, wildcard TLS qua **Cloudflare DNS-01**, CI/CD GitHub
  Actions (push `main` → test → build → ssh deploy).
- Cấu hình Caddy nằm ngoài project này: repo `devops/caddy`
  (`/home/mhc/projects/devops/caddy`; `Caddyfile`, `snippets/fileuploader.minhhoccode111.com`).
- Seed 2 account lúc startup: **admin** (active, role admin) + **demo** (active, role user,
  quota mặc định 5 GiB), credential demo hiển thị ở màn hình login.
- File lớn nhất đã test local: **6 GB**. Upload 6 GB trên prod bị chặn đúng bởi quota 5 GiB.
- Lý do chọn stack: muốn thử **SSR với Go + templ + htmx**.
- Default status của user mới là **`pending`** để chờ admin duyệt.

### 17.1. Vai trò & đóng góp

1. Phần nào bạn thực sự tự viết, phần nào AI viết? (Rất quan trọng để mô tả trung thực trên CV.)
2. Thời gian effort thực tế là bao lâu (số giờ/tuần), ngoài 43 ngày lịch từ git?
3. Phần nào bạn thiết kế từ đầu, phần nào kế thừa từ template?
4. Bạn có viết test hay chủ yếu nhờ AI? Bạn đã debug những bug khó nào?

### 17.2. Kết quả & số liệu (sau khi deploy)

5. Sau khi deploy, có bao nhiêu người dùng thật? Account demo được dùng thử bao nhiêu lần?
6. Dung lượng đã upload thực tế trên VPS? Disk/volume còn bao nhiêu?
7. Có số đo hiệu năng chưa: thời gian upload 6 GB, throughput download, p95 latency, request/s?
8. Có số liệu trước/sau tối ưu nào không (keyset pagination vs offset, rate limiter, ...)?
9. Có sự cố production nào đáng nhớ không? Cách debug/fix?
10. Coverage thực tế khi chạy cả integration test là bao nhiêu? (Repo hiện chỉ có số unit.)

### 17.3. Quyết định & so sánh

11. Vì sao chọn Fiber thay vì `net/http` hoặc framework khác?
12. Vì sao chọn MinIO thay vì lưu file trên disk của VPS?
13. Vì sao chọn Redis cho session/rate thay vì Postgres?
14. Vì sao chọn kiến trúc layered + interface thay vì đơn giản hơn?
15. Có phương án nào bạn đã cân nhắc nhưng bỏ? Vì sao?

### 17.4. Deploy & vận hành

16. ~~Snippet Caddy `fileuploader.minhhoccode111.com` đã tạo chưa?~~ **Đã tạo + deploy** (repo
    `devops/caddy`, rsync + `deploy.sh`); `request_body max_size 64MB` cho part 50 MiB.
17. ~~Snippet có set `X-Real-IP`/`X-Forwarded-For` chưa?~~ **Có set**, nhưng vẫn chưa trust
    Cloudflare ranges / dùng `{client_ip}` — login throttle trên prod đang gộp khách theo IP
    Cloudflare nếu bật proxied; cần xử lý nếu mở demo cho nhiều người.
18. Backup Postgres/MinIO volume bằng cách nào? Có monitor disk không? — **Chưa có** (mục 16.4).
19. ~~Bật `HTTP_FAKE_LATENCY_MAX=0`, `LOG_LEVEL=info`, đổi secret chưa?~~ **Đã set trên VPS `.env`**;
    secret sinh random, không dùng default.
20. Kế hoạch rollback khi deploy lỗi? — **Có**: pin `IMAGE_TAG=<sha>` trong `.env` trên VPS;
    image Docker Hub giữ tag sha mỗi lần push.

### 17.5. Hướng phát triển

22. Bạn định làm tiếp gì? (admin settings, email verification, resume upload, audit log, ...)
23. Bạn có ý định production hóa không? Nếu có, cần gì trước?
24. Bạn muốn nhấn mạnh điều gì nhất khi kể về dự án này trong phỏng vấn?

---

## Phụ lục A — Lệnh Makefile quan trọng

| Lệnh                                       | Tác dụng                   |
| ------------------------------------------ | -------------------------- |
| `make compose-up`                          | Postgres + Redis + MinIO   |
| `make compose-up-all`                      | Cả stack + app + nginx     |
| `make run`                                 | Chạy app với tag `migrate` |
| `make dev`                                 | air + Tailwind watch       |
| `make test` / `test-race` / `test-shuffle` | Unit/HTTP test             |
| `make test-integration`                    | Postgres + MinIO thật      |
| `make test-fuzz`                           | Fuzz cursor 10s            |
| `make test-cover`                          | Coverage profile           |
| `make format`                              | go fix + gofumpt + gci     |
| `make linter-golangci`                     | 47 linter                  |
| `make migrate-up` / `migrate-down`         | Một bước migration         |
| `make schema`                              | Dump schema                |
| `make pre-commit`                          | Full check                 |

Nguồn: `Makefile`.

## Phụ lục B — Biến môi trường chính

| Biến                                  | Default                                                | Ý nghĩa                        |
| ------------------------------------- | ------------------------------------------------------ | ------------------------------ |
| `HTTP_PORT`                           | 8090                                                   | Cổng app                       |
| `PG_URL`                              | `postgres://user:password@localhost:5433/fileuploader` | Postgres                       |
| `PG_POOL_MAX`                         | 10                                                     | Pool size                      |
| `REDIS_ADDR`                          | `localhost:6379`                                       | Redis                          |
| `MINIO_ENDPOINT`                      | `localhost:9000`                                       | S3 endpoint                    |
| `MINIO_BUCKET`                        | `files`                                                | Bucket                         |
| `UPLOAD_MAX_BYTES`                    | 10 GiB                                                 | Giới hạn file                  |
| `UPLOAD_PART_BYTES`                   | 50 MiB                                                 | Part size (5–50 MiB)           |
| `UPLOAD_SESSION_IDLE_TTL`             | 1h                                                     | Idle TTL                       |
| `UPLOAD_TIMEOUT`                      | 5m                                                     | Read timeout                   |
| `STORAGE_DEFAULT_QUOTA_BYTES`         | 0                                                      | Quota mặc định (0 = unlimited) |
| `UPLOAD_DEFAULT_RATE_BYTES_PER_SEC`   | 0                                                      | Upload rate mặc định           |
| `DOWNLOAD_DEFAULT_RATE_BYTES_PER_SEC` | 0                                                      | Download rate mặc định         |
| `SESSION_TTL`                         | 24h                                                    | Session TTL                    |
| `HTTP_FAKE_LATENCY_MIN/MAX`           | 100ms/200ms                                            | Fake latency                   |
| `OBSERVE_ENABLED`                     | true                                                   | Metrics/pprof listener         |
| `OBSERVE_ADDR`                        | 127.0.0.1:9090                                         | Listener address               |
| `LOG_LEVEL`                           | debug                                                  | Log level                      |

Nguồn: `config/config.go`, `.env.example`, `docker-compose.yml`.

---

_Hết tài liệu._
