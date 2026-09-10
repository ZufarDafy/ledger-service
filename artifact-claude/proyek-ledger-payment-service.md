# Proyek Unggulan — Ledger & Payment Service

**Nama repo yang disarankan:** `ledgerd` atau `pilar-ledger` — bukan `belajar-golang` atau `project-portofolio`. Nama repo adalah kesan pertama.

**Durasi:** 10 minggu · **Target selesai:** akhir Desember 2026
**Bahasa:** Go · **Database:** PostgreSQL

---

## Apa yang Dibangun (dan Apa yang Tidak)

Sebuah layanan buku besar (ledger) berpasangan dengan orkestrasi pembayaran sederhana. Permukaan fiturnya sengaja kecil; kedalaman rekayasanya yang besar.

**Termasuk:**
- Akun dan pencatatan double-entry yang tidak bisa diubah (append-only)
- API transfer dengan jaminan idempotensi
- Penanganan konkurensi pada saldo akun
- Pengiriman webhook dengan retry, backoff, dan dead letter queue
- Endpoint mutasi rekening dengan pagination efisien
- Job rekonsiliasi harian

**Sengaja TIDAK termasuk** — batasan ini yang membuat proyeknya selesai:
- Tidak ada UI. Sama sekali.
- Tidak ada integrasi payment gateway sungguhan (pakai provider tiruan)
- Tidak ada multi-mata uang atau konversi kurs
- Tidak ada KYC, tidak ada manajemen user (cukup API key)
- Tidak ada fitur "biar lengkap"

> Tulis batasan ini di README. Menyatakan apa yang sengaja tidak Anda kerjakan dan kenapa adalah sinyal kematangan teknis. Pewawancara membacanya sebagai orang yang bisa mengelola cakupan.

---

## Kenapa Domain Ini Memaksa Arsitektur yang Benar

Setiap komponen di bawah lahir dari masalah nyata, bukan ditempelkan supaya CV terlihat ramai. Ini bedanya proyek yang tahan pertanyaan dan yang tidak.

| Masalah nyata | Memaksa Anda memakai |
|---|---|
| Klien retry saat timeout, tidak boleh transfer dua kali | Idempotency key, unique constraint |
| Dua transfer bersamaan ke akun yang sama | Locking, isolation level, transaksi |
| Saldo tidak boleh pernah salah | Invariant double-entry, rekonsiliasi |
| Webhook harus terkirim persis jika transaksi commit | Transactional outbox |
| Penerima webhook sedang down | Retry, exponential backoff, DLQ |
| Mutasi rekening bisa jutaan baris | Keyset pagination, indexing, partisi |
| Audit trail wajib | Append-only, koreksi lewat reversing entry |

---

## Model Domain

**Aturan yang tidak boleh dilanggar:** setiap transaksi terdiri dari dua atau lebih posting yang jumlahnya **selalu nol**. Saldo tidak pernah di-`UPDATE` langsung — saldo adalah hasil turunan dari posting.

```
accounts
  id, type (asset|liability|revenue|expense), currency, created_at

transactions
  id, idempotency_key, status, description, created_at

postings              -- APPEND ONLY, tidak pernah UPDATE atau DELETE
  id, transaction_id, account_id, amount_minor (int64), created_at
  CONSTRAINT: SUM(amount_minor) per transaction_id = 0

account_balances      -- materialized, untuk baca cepat
  account_id, balance_minor, version, updated_at

idempotency_keys
  key, request_hash, response_body, status_code, created_at

outbox                -- event menunggu dikirim
  id, aggregate_id, event_type, payload, published_at

webhook_deliveries
  id, endpoint_id, event_id, attempt, next_retry_at, status, last_error
```

**Uang selalu `int64` dalam satuan terkecil (sen/rupiah).** Tidak pernah `float`. Ini pertanyaan interview klasik di fintech, dan jawaban Anda harus datang dari pengalaman, bukan hafalan.

---

## Enam Tantangan Teknis Inti

Ini isi sesungguhnya dari proyek ini. Setiap satu menghasilkan bahan cerita interview.

### 1. Idempotensi
Klien mengirim `Idempotency-Key` di header. Simpan hash request bersama key. Kalau key sama datang lagi dengan body sama, kembalikan respons tersimpan tanpa mengeksekusi ulang. Kalau key sama tapi body berbeda, tolak dengan `422`.

Yang sulit: dua request identik datang **bersamaan**. Unique constraint pada `key` plus penanganan conflict yang benar. Uji ini secara eksplisit.

### 2. Konkurensi pada saldo
Ini inti proyeknya. Dua transfer ke akun yang sama pada saat bersamaan tidak boleh menyebabkan lost update.

Implementasikan **dua pendekatan**, ukur keduanya, tulis hasilnya:
- **Pessimistic**: `SELECT ... FOR UPDATE`, dengan akun dikunci dalam urutan ID terurut untuk mencegah deadlock
- **Optimistic**: kolom `version`, retry saat konflik

Bandingkan throughput dan tingkat retry di bawah kontensi tinggi dan rendah. Grafik perbandingan ini akan jadi salah satu hal paling menarik di README Anda.

### 3. Transactional outbox
Masalahnya: kalau Anda commit transaksi database lalu publish event ke antrean, dan proses mati di antara keduanya, event hilang. Kalau publish dulu lalu commit gagal, event palsu terkirim.

Solusinya: tulis event ke tabel `outbox` **di dalam transaksi database yang sama**. Worker terpisah membaca outbox dengan `SELECT ... FOR UPDATE SKIP LOCKED` dan menerbitkannya.

Ini pola yang dipakai sistem pembayaran sungguhan. Bisa menjelaskannya dengan lancar akan langsung membedakan Anda dari kandidat lain di level yang sama.

### 4. Pengiriman webhook
Exponential backoff dengan jitter, batas percobaan, dead letter queue, penandatanganan HMAC-SHA256 pada payload, dan header timestamp untuk mencegah replay.

Pengiriman bersifat *at-least-once* — dokumentasikan bahwa penerima wajib idempoten. Menyatakan jaminan Anda secara eksplisit adalah tanda orang yang paham sistem terdistribusi.

### 5. Optimasi query
Endpoint mutasi rekening dengan **keyset pagination**, bukan `OFFSET`. Jalankan `EXPLAIN ANALYZE`, catat rencana eksekusi sebelum dan sesudah indexing, masukkan hasilnya ke dokumentasi.

Isi tabel dengan 10 juta baris posting untuk pengujian. Perbedaan performa pada volume kecil tidak membuktikan apa pun.

### 6. Rekonsiliasi
Job harian yang memverifikasi: jumlah semua posting per transaksi = 0, saldo materialized cocok dengan hasil hitung ulang dari posting, dan tidak ada transaksi menggantung. Terbitkan metrik saat ditemukan ketidakcocokan.

---

## Arsitektur Layanan

Tiga layanan — cukup untuk menjustifikasi gRPC, tidak sampai jadi beban:

```
  ┌─────────────┐   REST    ┌──────────────┐   gRPC   ┌─────────────┐
  │   Client    │ ────────▶ │ api-gateway  │ ───────▶ │   ledger    │
  └─────────────┘           └──────────────┘          │   service   │
                                                      └──────┬──────┘
                                                             │
                                                      ┌──────▼──────┐
                                                      │ PostgreSQL  │
                                                      │  + outbox   │
                                                      └──────┬──────┘
                                                             │
                                                      ┌──────▼──────┐
                                                      │  webhook    │
                                                      │   worker    │
                                                      └─────────────┘
```

**gRPC untuk internal, REST untuk publik.** Tulis alasannya di ADR — ini pola standar industri dan Anda harus bisa mempertahankannya.

---

## Stack

| Lapisan | Pilihan | Catatan |
|---|---|---|
| Bahasa | Go 1.23+ | Dominan di lowongan backend Indonesia |
| HTTP | chi | Ringan, idiomatik |
| RPC | grpc-go + protobuf | |
| Database | PostgreSQL 16 | |
| Akses DB | **sqlc** + pgx | Hindari ORM berat; reviewer menyukai SQL eksplisit |
| Migrasi | golang-migrate | |
| Antrean | Outbox + SKIP LOCKED (v1), NATS JetStream (v2) | Mulai sederhana, tulis ADR kapan broker sungguhan diperlukan |
| Test | testify + **testcontainers-go** | Integration test melawan Postgres sungguhan, bukan mock |
| Load test | k6 | |
| Observability | Prometheus + Grafana + OpenTelemetry | |
| CI/CD | GitHub Actions | |
| Deploy | Docker → GKE atau Cloud Run | Sekalian latihan ACE |

---

## Strategi Testing

Ini muncul di hampir setiap deskripsi lowongan yang saya periksa, dan repo Anda saat ini tidak punya satu pun. Perlakukan sebagai fitur utama, bukan pelengkap.

- **Unit** — logika domain murni: validasi entri, aturan penjumlahan nol, aritmetika uang
- **Integration** — testcontainers dengan Postgres asli. Bukan mock database; mock tidak membuktikan SQL Anda benar.
- **Uji konkurensi** ← yang paling penting
  100 goroutine melakukan transfer bersamaan pada akun yang sama. Assert: total saldo tidak berubah, tidak ada lost update, semua posting berjumlah nol.
  **Test ini sendirian bisa membawa Anda melewati satu interview.** Sangat sedikit kandidat level junior yang pernah menulisnya.
- **E2E** — alur penuh: API → ledger → outbox → webhook diterima server uji
- Target cakupan: 70%+ pada paket domain. Pasang badge coverage di README.

---

## Jadwal 10 Minggu

Beban depan sengaja berat karena cohort ACE mulai 12 Oktober.

| Minggu | Periode | Fokus | Jam |
|---|---|---|---|
| 1 | 1–7 Sep | Model domain, skema, migrasi, logika posting, unit test | 15 |
| 2 | 8–14 Sep | API transfer, idempotensi, **penanganan konkurensi + uji konkurensi** | 15 |
| 3 | 15–21 Sep | Pisah jadi gRPC, integration test dengan testcontainers | 15 |
| 4 | 22–28 Sep | Outbox, webhook worker, retry/backoff/DLQ, HMAC | 15 |
| 5 | 29 Sep–5 Okt | Endpoint mutasi, keyset pagination, indexing, EXPLAIN ANALYZE | 15 |
| 6 | 6–12 Okt | Job rekonsiliasi, Docker, GitHub Actions | 12 |
| 7 | 13–19 Okt | Deploy ke GCP *(cohort ACE mulai — turunkan tempo)* | 7 |
| 8 | 20 Okt–2 Nov | Observability: metrik, trace, dashboard Grafana | 7 |
| 9 | 3–16 Nov | **Load test k6 + uji kegagalan**, catat semua angka | 7 |
| 10 | 17–30 Nov | Dokumentasi, ADR, diagram, artikel blog | 7 |
| — | Des | Cadangan. Selalu terpakai. | — |

**Commit publik sejak hari pertama.** Riwayat commit yang rapi selama tiga bulan adalah bukti konsistensi yang tidak bisa dipalsukan.

---

## Uji Kegagalan (Minggu 9)

Jangan lewatkan bagian ini. Di sinilah proyek berubah dari "jalan" jadi "terbukti".

- Matikan webhook worker di tengah beban → assert tidak ada event hilang, outbox tetap konsisten
- Putuskan koneksi database saat transaksi berjalan → assert tidak ada saldo terkorupsi
- Buat endpoint webhook penerima mengembalikan 500 terus-menerus → assert masuk DLQ setelah N percobaan
- Jalankan dua instance ledger service bersamaan → assert tidak ada double-spend

Dokumentasikan **apa yang terjadi**, bukan apa yang seharusnya terjadi. Kalau ada yang rusak, perbaiki lalu tulis ceritanya. Cerita "saya menemukan bug ini dan begini cara memperbaikinya" adalah bahan interview terbaik yang bisa Anda punya.

---

## Dokumentasi

**ADR yang harus ditulis** (masing-masing 1 halaman: konteks, pilihan yang dipertimbangkan, keputusan, konsekuensi):

1. Double-entry vs kolom saldo tunggal
2. Pessimistic vs optimistic locking — **sertakan data pengukuran**
3. Transactional outbox vs publish langsung
4. `int64` satuan terkecil vs tipe decimal
5. gRPC internal, REST eksternal
6. Keyset vs offset pagination
7. Kapan antrean sungguhan (NATS) diperlukan, dan kenapa belum sekarang

**README wajib memuat:**
- Masalah apa yang dipecahkan, dalam 3 kalimat
- Diagram arsitektur
- Cara menjalankan (`docker compose up` harus cukup)
- **Hasil load test dengan angka**: throughput, p50/p95/p99, perilaku di bawah kontensi
- Batasan yang diketahui dan apa yang akan diubah pada skala 100×

---

## Yang Anda Dapat di Akhir

Baris CV:
> *Membangun layanan ledger double-entry di Go dengan jaminan idempotensi dan konsistensi di bawah konkurensi. Menangani N transaksi/detik dengan p95 X ms. Pengiriman webhook lewat transactional outbox dengan jaminan at-least-once.*

Dan lebih penting dari itu — tujuh cerita interview yang tidak bisa dikarang orang lain:

1. Bagaimana Anda mencegah double-spend di bawah konkurensi
2. Kenapa Anda memilih pessimistic locking, dengan data pengukurannya
3. Kenapa transactional outbox, bukan publish langsung
4. Bagaimana Anda menjamin idempotensi pada request bersamaan
5. Bagaimana Anda mengoptimasi query mutasi dari X ms ke Y ms
6. Apa yang terjadi saat Anda mematikan worker di tengah beban
7. Kenapa uang tidak boleh disimpan sebagai float

Setiap satu adalah jawaban atas pertanyaan yang benar-benar ditanyakan perusahaan fintech.

---

## Langkah Pertama

Minggu ini: buat repo, tulis skema database dan migrasi, implementasikan logika posting dengan invariant penjumlahan nol, dan tulis unit test-nya. Tidak ada API dulu, tidak ada Docker dulu.

Kalau model domainnya benar, sisanya menyusul. Kalau salah, semua yang dibangun di atasnya ikut salah.
