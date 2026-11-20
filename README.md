# FastAsiaPanel

[English](#english) | [فارسی](#فارسی)

---

## Quick Install / نصب سریع روی سرور

```bash
bash <(curl -Ls https://raw.githubusercontent.com/VahidDashtbani/fastasiapanel/main/install.sh)
```

---

<a id="english"></a>
## English

**FastAsiaPanel** is a lightweight, modern Xray management panel written in Go with a signature **Aurora Glassmorphism** web UI and an interactive CLI manager (`fastasian`).

### Key Features
- **Protocols & Transports:** VLESS, VMess, Trojan, and Shadowsocks over TCP, WebSocket, gRPC, and xHTTP with TLS and Reality (`X25519` key generation).
- **Advanced Outbounds & Chaining:** Dedicated Outbounds manager supporting multi-hop relay chains (`relay_tag`) and custom stream settings.
- **Routing Rules & Quota Groups:** Independent Routing Rules engine (domains, IPs, ports, protocols, priority) and shared Quota Groups.
- **Client Management & Live Traffic:** Per-client volume limits, expiration dates, automatic periodic traffic reset, IP limits, real-time live traffic sync, Subscription URLs (`/sub/{subId}`), and scannable QR codes.
- **Relational Databases:** Full relational schema for **SQLite** (`/etc/fastasiapanel/fastasiapanel.db`) and **PostgreSQL**.
- **Interactive CLI (`fastasian`):** Manage panel & Xray services, port, Web Base Path, SSL certificates (`acme.sh`), UFW firewall, TCP BBR, GeoIP/GeoSite updates, and panel upgrades.

### CLI Management
```bash
fastasian                 # Open interactive management menu
fastasian status          # Check panel and Xray service status
fastasian settings        # View panel URL, port, DB, and credentials
fastasian restart         # Restart FastAsiaPanel service
fastasian update          # Update FastAsiaPanel to the latest release
fastasian log             # Follow live service logs
```

### Environment Variables
| Variable | Default | Description |
|---|---|---|
| `FAP_LISTEN` | `0.0.0.0:2053` | Panel listen address and port |
| `FAP_BASE_PATH` | `/` | Secret web base path (e.g., `/x7k2p9`) |
| `FAP_ADMIN_USER` / `FAP_ADMIN_PASS` | `admin` / - | Initial administrator credentials |
| `FAP_PUBLIC_HOST` | Request host | Public domain or IP used in client & subscription links |
| `FAP_DB_TYPE` | `sqlite` | Database backend (`sqlite` or `postgres`) |
| `FAP_DB_PATH` | `/etc/fastasiapanel/fastasiapanel.db` | SQLite database file path |
| `FAP_DB_DSN` | - | PostgreSQL connection string |
| `FAP_XRAY_BIN` | `/usr/local/fastasiapanel/bin/xray` | Path to Xray core binary |

### Local Development
```bash
go mod tidy
go vet ./...
go test ./...
go run ./cmd/panel
```

---

<a id="فارسی"></a>
## فارسی

**FastAsiaPanel** یک پنل مدیریت سبک، سریع و مدرن برای هسته Xray به زبان Go است که دارای رابط کاربری شیشه‌ای (**Aurora Glassmorphism**) و ابزار مدیریت خط فرمان تعاملی (`fastasian`) می‌باشد.

### امکانات کلیدی
- **پروتکل‌ها و ترنسپورت‌ها:** پشتیبانی کامل از VLESS، VMess، Trojan و Shadowsocks روی بسترهای TCP، WebSocket، gRPC و xHTTP به همراه امنیت TLS و Reality (ساخت خودکار کلید `X25519` و `Short ID`).
- **مدیریت مجزای Outbound و زنجیره‌سازی (Relay):** بخش مستقل برای تعریف Outboundهای سفارشی و اتصال چندلایه (`relay_tag`).
- **قوانین مسیریابی و گروه‌های حجمی:** بخش مجزای Routing Rules (فیلتر دامنه، IP، پورت، پروتکل و اولویت) و گروه‌های اشتراک حجم (Quota Groups).
- **مدیریت کاربران و ترافیک زنده:** محدودیت حجم و تاریخ انقضا، ریست دوره‌ای خودکار ترافیک، نمایش لحظه‌ای مصرف کاربران (Live Traffic)، لینک اشتراک (`/sub/{subId}`) و QR کد واقعی قابل اسکن در کلاینت‌ها.
- **دیتابیس رابطه‌ای (SQLite و PostgreSQL):** پشتیبانی کامل از **SQLite** در مسیر `/etc/fastasiapanel/fastasiapanel.db` و **PostgreSQL** با ساختار جداول استاندارد.
- **مدیریت سرور با دستور `fastasian`:** کنترل سرویس پنل و Xray، تغییر پورت و مسیر ورود (`WebBasePath`)، صدور گواهی SSL با `acme.sh`، مدیریت فایروال UFW، فعال‌سازی TCP BBR و آپدیت خودکار پنل.

### دستورات مدیریت با `fastasian`
```bash
fastasian                 # باز کردن منوی تعاملی مدیریت پنل
fastasian status          # مشاهده وضعیت سرویس پنل و Xray
fastasian settings        # نمایش آدرس پنل، پورت، دیتابیس و مشخصات ورود
fastasian restart         # ریستارت سرویس FastAsiaPanel
fastasian update          # به‌روزرسانی پنل به آخرین نسخه منتشرشده
fastasian log             # مشاهده زنده لاگ‌های سرویس
```

### متغیرهای محیطی مهم
| متغیر | مقدار پیش‌فرض | توضیح |
|---|---|---|
| `FAP_LISTEN` | `0.0.0.0:2053` | آدرس و پورت گوش‌دهنده پنل |
| `FAP_BASE_PATH` | `/` | مسیر مخفی ورود به پنل (مثلاً `/x7k2p9`) |
| `FAP_ADMIN_USER` / `FAP_ADMIN_PASS` | `admin` / - | نام کاربری و رمز عبور اولیه ادمین |
| `FAP_PUBLIC_HOST` | هاست درخواست | دامنه یا IP عمومی برای ساخت لینک کلاینت‌ها و ساب |
| `FAP_DB_TYPE` | `sqlite` | نوع دیتابیس (`sqlite` یا `postgres`) |
| `FAP_DB_PATH` | `/etc/fastasiapanel/fastasiapanel.db` | مسیر فایل دیتابیس SQLite |
| `FAP_DB_DSN` | - | رشته اتصال دیتابیس PostgreSQL |
| `FAP_XRAY_BIN` | `/usr/local/fastasiapanel/bin/xray` | مسیر فایل اجرایی هسته Xray |

### اجرای محلی برای توسعه
```bash
go mod tidy
go vet ./...
go test ./...
go run ./cmd/panel
```