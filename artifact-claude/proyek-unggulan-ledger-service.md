# Proyek Unggulan — Ledger & Payment Service

**Bahasa:** Go · **Durasi:** 10 minggu · **Alokasi:** 15+ jam/minggu
**Tujuan:** portofolio yang membuktikan kemampuan backend, DevOps, dan system design untuk lamaran fintech tingkat Prioritas 1

---

## Kenapa Proyek Ini

Setiap keputusan arsitektur di sini **dipaksa oleh masalahnya**, bukan ditempelkan supaya terlihat canggih. Itu pembeda utamanya saat diwawancarai.

- Uang tidak boleh hilang atau berlipat → memaksa transaksi database, isolation level, dan double-entry
- Klien akan melakukan retry → memaksa idempotensi
- Webhook tidak boleh memblokir transaksi, dan harus dicoba ulang berjam-jam → memaksa antrean dan outbox pattern
- Beberapa layanan berbagi kontrak ketat → memaksa gRPC
- Akun populer akan diakses bersamaan → memaksa strategi penguncian

Pertanyaan "kenapa Anda pakai antrean di sini?" akan bisa Anda jawab dengan alasan, bukan dengan "supaya lengkap".

**Peringatan kejujuran:** ini sistem pembelajaran, bukan sistem pembayaran produksi. Tulis itu di README. Mengaku membangun payment gateway sungguhan akan langsung dibongkar oleh pewawancara fintech mana pun.

---

## Ruang Lingkup

### Yang dibangun
- Buku besar berpasangan (double-entry) — setiap transaksi punya ≥2 entri yang jumlahnya nol
- Transfer antar akun dengan idempotency key
- Riwayat transaksi dan saldo akun
- Pengiriman webhook dengan retry, exponential backoff, dan dead letter queue
- Job rekonsiliasi harian yang memverifikasi invarian buku besar
- API publik dengan autentikasi API key dan rate limiting

### Yang **tidak** dibangun (tulis ini di README juga)
- Frontend — nol. Ini proyek backend.
- Integrasi ke rail pembayaran nyata
- Manajemen pengguna di luar API key
- Konversi mata uang / FX
- Fitur apa pun yang tidak menambah kedalaman rekayasa

> Disiplin cakupan adalah bagian dari penilaian. Satu fitur yang ditangani sampai ke lapisan konkurensi, kegagalan, dan observability jauh lebih meyakinkan daripada sepuluh fitur dangkal.

---

## Arsitektur

Tiga layanan plus satu job terjadwal:

| Layanan | Protokol | Tanggung jawab |
|---|---|---|
| `ledger-core` | gRPC | Satu-satunya yang boleh menulis entri. Pemilik database. Menjaga invarian. |
| `api-gateway` | HTTP/REST | API publik. Auth, idempotensi, rate limiting, validasi. Memanggil `ledger-core`. |
| `webhook-dispatcher` | worker | Konsumsi event dari antrean, kirim webhook, retry, DLQ. |
| `reconciler` | cron job | Verifikasi harian: total debit = total kredit, saldo cocok dengan entri. |

Pemisahan ini punya alasan yang bisa dipertahankan: hanya satu proses yang boleh menyentuh integritas buku besar, sementara lalu lintas publik dan pengiriman webhook punya karakteristik beban dan mode kegagalan yang sama sekali berbeda.

### Model data inti

```
accounts       (id, currency, created_at)
entries        (id, transaction_id, account_id, amount, direction, created_at)
transactions   (id, idempotency_key, status, created_at)
outbox         (id, aggregate_id, payload, published_at)
webhook_attempts (id, event_id, attempt_no, status, next_retry_at)
```

**Aturan yang tidak bisa dilanggar:**
- Tabel `entries` bersifat **append-only**. Tidak ada UPDATE, tidak ada DELETE. Koreksi dilakukan dengan entri lawan.
- Uang disimpan sebagai `BIGINT` dalam satuan terkecil (sen/rupiah). **Tidak pernah float.**
- `SUM(amount)` seluruh entri dalam satu transaksi harus = 0. Ini invarian utama.

---

## Delapan Keputusan yang Harus Anda Tulis sebagai ADR

Bagian ini yang membuat proyek Anda berbeda dari ribuan repo lain. Setiap ADR: konteks, opsi yang dipertimbangkan, keputusan, konsekuensi.

1. **Double-entry vs kolom saldo tunggal.** Kenapa entri immutable lebih baik daripada `UPDATE accounts SET balance = balance - x`. Jejak audit, kemampuan rekonstruksi, dan hilangnya seluruh kelas bug lost-update.

2. **Representasi uang.** Kenapa `BIGINT` satuan minor, bukan `FLOAT` atau `DECIMAL`. Sertakan contoh nyata kegagalan floating point.

3. **Kontrol konkurensi.** `SELECT ... FOR UPDATE` vs optimistic locking vs isolation `SERIALIZABLE`. Jelaskan juga **urutan penguncian kanonik** (kunci akun berdasarkan urutan ID) untuk mencegah deadlock pada transfer silang A→B dan B→A yang bersamaan. Ini detail yang membuat pewawancara fintech duduk tegak.

4. **Idempotensi.** Bagaimana kunci disimpan, constraint unik apa yang dipakai, dan apa yang terjadi saat dua request identik datang **bersamaan**, bukan berurutan. Kasus balapan inilah yang menarik.

5. **Transactional outbox.** Kenapa event ditulis ke tabel `outbox` dalam transaksi database yang sama dengan entri, lalu dipublikasikan terpisah — bukan dikirim langsung ke antrean. Ini menyelesaikan masalah dual-write, dan pemahaman ini menandai engineer yang serius.

6. **Perhitungan saldo.** Dihitung dari agregasi entri vs snapshot yang dipelihara. Kapan agregasi jadi terlalu lambat, dan bagaimana snapshot inkremental menyelesaikannya tanpa mengorbankan jejak audit.

7. **Kebijakan retry webhook.** Exponential backoff dengan jitter, batas percobaan, DLQ. Kenapa pengiriman at-least-once dan penerima wajib idempoten.

8. **PostgreSQL, bukan MongoDB.** Kenapa jaminan transaksional adalah syarat mati di domain ini.

---

## Rencana 10 Minggu

### Minggu 1 — Fondasi
Desain domain dan skema. Struktur repo. Migrasi database. Skeleton CI di GitHub Actions sejak hari pertama, bukan belakangan. **ADR 1, 2, 8.**

### Minggu 2 — Inti buku besar
`ledger-core` sebagai layanan gRPC. Penulisan entri, penegakan invarian, pembuatan transaksi. Unit test untuk logika domain.

### Minggu 3 — Konkurensi ← minggu terpenting
Implementasi penguncian dan urutan kunci kanonik. Integration test memakai testcontainers dengan Postgres asli, bukan mock. **Tulis tes konkurensi**: 100 goroutine melakukan transfer bersamaan, lalu pastikan jumlah seluruh entri tetap nol dan tidak ada saldo yang hilang. Uji juga skenario deadlock A→B dan B→A. **ADR 3.**

### Minggu 4 — API dan idempotensi
`api-gateway`: REST, autentikasi API key, rate limiting, validasi input ketat. Middleware idempotensi. Uji request duplikat, berurutan maupun bersamaan. **ADR 4.**

### Minggu 5 — Outbox dan antrean
Tabel outbox ditulis dalam transaksi yang sama. Publisher terpisah. `webhook-dispatcher` mengonsumsi, mengirim, retry dengan backoff, dan memindahkan kegagalan ke DLQ. **ADR 5, 7.**

### Minggu 6 — Rekonsiliasi dan performa query
Job rekonsiliasi harian. Snapshot saldo. Jalankan `EXPLAIN ANALYZE` pada query berat, tambahkan index, **catat sebelum-sesudah dalam angka**. Ini bahan cerita interview database yang sangat kuat. **ADR 6.**

### Minggu 7 — Kontainer dan deployment
Dockerfile multi-stage. `docker-compose` untuk lokal. Manifest Kubernetes. Pipeline GitHub Actions lengkap: lint → test → build → push → deploy. Deploy ke GCP.

### Minggu 8 — Observability
Metrik Prometheus: throughput transaksi, histogram latency, kedalaman antrean, tingkat keberhasilan webhook, jumlah retry. Structured logging dengan request ID yang menembus semua layanan. Dashboard Grafana. Health dan readiness probe.

### Minggu 9 — Pembuktian ← minggu yang menghasilkan bahan wawancara
Load test dengan k6. Lalu **eksperimen kegagalan yang sengaja dilakukan dan didokumentasikan**:

- Matikan `webhook-dispatcher` di tengah pengiriman → buktikan at-least-once tetap terjaga
- Kirim dua request identik dengan idempotency key sama secara bersamaan → buktikan hanya satu transaksi terbentuk
- Beri beban ke satu akun panas → ukur penurunan throughput akibat kontensi, lalu jelaskan penyebabnya
- Putuskan koneksi database saat transaksi berjalan → amati dan dokumentasikan perilakunya

Catat semua angkanya: throughput, p95, p99, tingkat error.

### Minggu 10 — Dokumentasi
Diagram arsitektur. Delapan ADR dirapikan. README utama: masalah, arsitektur, hasil load test, hasil eksperimen kegagalan, dan **apa yang akan diubah kalau skala naik 100×**. Rekam video demo 3 menit.

---

## Jalur Paralel: ACE Mandiri

Kalau cohort GEAR batal, 9 jam/minggu yang tadinya dialokasikan ke sana bisa dipakai untuk:

- Menjalankan proyek ini di GCP dengan Free Tier $300 (pasang budget alert)
- Cloud SQL untuk Postgres, Pub/Sub untuk antrean, GKE atau Cloud Run untuk layanan, Cloud Monitoring untuk metrik
- Skills Boost untuk topik yang tidak tersentuh proyek (IAM, VPC, penagihan)
- Ujian ACE dibayar sendiri, ±Rp 2 juta, target Desember

Ini bukan penurunan kualitas. Membangun sistem nyata di GCP adalah persiapan ACE yang lebih baik daripada mengerjakan lab terpandu.

---

## Kriteria Selesai

Proyek dianggap selesai kalau seorang engineer senior yang belum pernah melihatnya bisa:

- [ ] Menjalankannya secara lokal dengan satu perintah
- [ ] Memahami arsitekturnya dari README dalam 5 menit
- [ ] Menemukan alasan setiap keputusan besar di ADR
- [ ] Melihat angka nyata dari load test dan eksperimen kegagalan
- [ ] Melihat test yang benar-benar menguji konkurensi, bukan sekadar happy path

Dan Anda bisa membicarakannya selama 20 menit tanpa kehabisan bahan.

---

## Kesalahan yang Harus Dihindari

**Menambah fitur.** Setiap kali tergoda menambahkan sesuatu, tanya: apakah ini menambah kedalaman rekayasa atau cuma menambah permukaan? Kalau permukaan, jangan.

**Menunda testing.** Test ditulis bersamaan dengan kode, bukan di minggu 9. Testing muncul di hampir setiap deskripsi lowongan yang kita lihat.

**Menunda dokumentasi.** Tulis ADR saat keputusan diambil, selagi alasannya masih segar. ADR yang ditulis ulang dari ingatan terbaca hambar.

**Membangun frontend.** Nol. Kalau butuh demo, pakai file `.http` atau koleksi Postman.

**Menyembunyikan kegagalan.** Kalau eksperimen menunjukkan sistem Anda kehilangan pesan dalam kondisi tertentu, **tulis itu di README** beserta rencana perbaikannya. Kejujuran teknis terbaca sebagai kematangan, bukan kelemahan.

---

## Langkah Pertama Minggu Ini

1. Pilih nama repo dan buat repositorinya — publik sejak awal
2. Tulis README kosong berisi pernyataan masalah dan daftar non-goals
3. Rancang skema database, tulis migrasi pertama
4. Pasang GitHub Actions dengan lint dan test, meski belum ada kode yang diuji
5. Tulis ADR 1 dan 2

Commit sejak hari pertama. Riwayat commit yang panjang dan konsisten adalah sinyal tersendiri.
