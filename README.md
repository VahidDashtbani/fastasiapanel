# FastAsiaPanel

پنل مدیریت Xray با پشتیبانی از VLESS، VMess، Trojan و Shadowsocks، ترنسپورت‌های TCP و WebSocket و gRPC، امنیت TLS و Reality، و محدودیت حجم و تاریخ انقضا برای هر کاربر.

## اجرا برای توسعه

```
go mod tidy
go vet ./...
go test ./...
PANEL_PASSWORD='یک-رمز-حداقل-۱۲-کاراکتری' XRAY_BIN=/path/to/xray go run ./cmd/panel
```

بعد از اجرا آدرس `http://127.0.0.1:8080/` را باز کنید. نام کاربری پیش‌فرض `admin` است. رمز فقط در اولین اجرا خوانده می‌شود و بعد از آن در دیتابیس (به‌صورت هش bcrypt) نگه داشته می‌شود.

## متغیرهای محیطی

| متغیر | پیش‌فرض | توضیح |
|---|---|---|
| `PANEL_ADDR` | `:8080` | آدرس و پورت پنل |
| `PANEL_PATH` | خالی | مسیر مخفی، مثل `/x7k2p9`. بدون آن، همه‌ی مسیرها زیر `/` هستند |
| `PANEL_USERNAME` / `PANEL_PASSWORD` | `admin` / - | فقط برای ساخت ادمین اولیه |
| `PANEL_PUBLIC_HOST` | هاست درخواست | آدرسی که در لینک کاربران می‌آید |
| `PANEL_TLS_CERT` / `PANEL_TLS_KEY` | - | فعال‌سازی HTTPS برای خود پنل |
| `DB_TYPE` | `json` | یکی از `json`، `sqlite` یا `postgres` |
| `DB_FILE` | `./data/fastasiapanel.json` (یا `.db` برای sqlite) | مسیر فایل دیتابیس |
| `DB_HOST` `DB_PORT` `DB_USER` `DB_PASS` `DB_NAME` | `127.0.0.1` `5432` `fastasia` - `fastasiapanel` | تنظیمات Postgres |
| `XRAY_BIN` | `xray` | مسیر باینری Xray |
| `XRAY_CONFIG` | `./data/xray.json` | مسیر کانفیگ تولیدشده |

## دستورهای باینری

```
fastasiapanel version
fastasiapanel reset-admin <username> <password>
```

## نصب روی سرور

بعد از ساخت Release (تگ `vX.Y.Z`) روی GitHub:

```
bash install.sh
bash install.sh --db postgres --port 2053 --db-port 5433
```

نصاب دیتابیس (SQLite یا PostgreSQL) و پورت‌ها را می‌پرسد، سرویس systemd می‌سازد و دستور `fastpanel` را نصب می‌کند. مقدار `REPO` بالای `install.sh` را به نام مخزن خودتان تغییر دهید.

## ساختار پروژه

```
cmd/panel            نقطه‌ی ورود و سیم‌کشی
internal/config      خواندن تنظیمات از env
internal/store       State و Backend (json، sqlite، postgres)
internal/auth        سشن، bcrypt، قفل تلاش ناموفق، بررسی Origin
internal/xray        ساخت کانفیگ، لینک‌ها، کلید Reality، مدیریت پروسه و آمار
internal/web         API و سرو فایل‌های UI
web                  index.html، login.html، app.js، login.js، style.css
```

## API

همه‌ی مسیرها زیر `PANEL_PATH` هستند و به جز `login` به سشن نیاز دارند.

| مسیر | کار |
|---|---|
| `POST /api/login` · `POST /api/logout` · `POST /api/password` | احراز هویت |
| `GET /api/status` | وضعیت پنل و Xray |
| `GET/POST /api/inbounds` | لیست (با کاربران و لینک‌ها) و ساخت |
| `PUT/DELETE /api/inbounds/{id}` · `POST /api/inbounds/{id}/toggle` | ویرایش، حذف، فعال/غیرفعال |
| `POST /api/clients` · `PUT/DELETE /api/clients/{id}` | کاربران |
| `POST /api/clients/{id}/reset` | ریست ترافیک |
| `GET /api/reality/keys` | ساخت کلید X25519 و Short ID |
| `POST /api/xray/restart` · `GET /api/backup` | ریستارت و بکاپ |

## محدودیت‌های شناخته‌شده

- با هر تغییر در اینباندها یا کاربران، Xray (با ۱.۵ ثانیه تاخیر برای ادغام تغییرات پشت‌سرهم) ریستارت می‌شود و اتصال‌های فعال یک لحظه قطع می‌شوند. افزودن زنده‌ی کاربر با gRPC در نسخه‌ی بعد.
- آمار ترافیک با `xray api statsquery` هر ۱۰ ثانیه خوانده می‌شود.
- هنوز ساب‌لینک، ربات تلگرام، محدودیت IP، ACME، قوانین routing سفارشی، QR و آمار CPU/RAM ندارد.
- انتقال خودکار داده بین JSON، SQLite و Postgres هنوز دستور CLI ندارد (تابع `Store.Replace` آماده است).
