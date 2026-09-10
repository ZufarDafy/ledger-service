# Minggu 1 — Fondasi

Target akhir minggu: repo publik dengan skema database, migrasi pertama, CI yang hijau, dan dua ADR. **Belum ada logika bisnis.** Itu Minggu 2.

---

## 1. Keputusan Tooling

Rekomendasi ini bukan selera — semuanya diambil dari stack yang muncul di lowongan yang kita periksa.

| Kebutuhan | Pilihan | Alasan |
|---|---|---|
| Go | 1.23+ | Routing di `net/http` sudah memadai sejak 1.22 |
| HTTP router | `chi` atau `net/http` saja | Ringan, idiomatis. Hindari framework berat. |
| Driver Postgres | `pgx/v5` | Standar de facto. Jangan `lib/pq` (sudah tidak dikembangkan). |
| Akses query | **`sqlc`** | Generate kode Go dari SQL mentah, type-safe |
| Migrasi | `goose` atau `golang-migrate` | Keduanya baik, pilih satu |
| gRPC | `grpc-go` + `buf` | `buf` untuk codegen dan linting proto |
| Logging | `log/slog` (stdlib) | Structured logging tanpa dependensi |
| Testing | stdlib + `testify` + `testcontainers-go` | Postgres asli di test, bukan mock |
| Metrik | `prometheus/client_golang` | Standar |
| Config | environment variable | Tanpa framework |

### Jangan pakai ORM

Ini penting, dan alasannya bukan ideologis. GORM menyembunyikan SQL yang dihasilkannya. Di Minggu 6 Anda akan menjalankan `EXPLAIN ANALYZE`, menambahkan index, dan mencatat perbaikan performa dalam angka — itu mustahil kalau Anda tidak tahu query apa yang sebenarnya jalan. Dan saat pewawancara bertanya bagaimana Anda menangani kunci baris, jawaban "GORM yang urus" adalah akhir percakapan.

`sqlc` memberi Anda SQL mentah plus keamanan tipe. Ini kompromi yang tepat untuk proyek ini.

---

## 2. Struktur Repo

```
ledger/
├── cmd/
│   ├── ledger-core/       # layanan gRPC
│   ├── api-gateway/       # REST publik
│   ├── dispatcher/        # worker webhook
│   └── reconciler/        # cron job
├── internal/
│   ├── ledger/            # logika domain — inti proyek
│   ├── storage/           # kode hasil generate sqlc + repository
│   ├── outbox/
│   └── webhook/
├── migrations/
├── proto/
├── docs/
│   ├── adr/
│   └── architecture.md
├── docker-compose.yml
├── Makefile
└── README.md
```

`internal/ledger` harus **tidak tahu apa-apa soal HTTP, gRPC, atau Postgres**. Logika domain murni, bisa diuji tanpa database. Pemisahan ini yang membuat unit test Anda cepat dan bermakna.

---

## 3. Draf Skema

Ini titik awal, bukan jawaban. Bagian **Pertanyaan Terbuka** di bawah harus Anda putuskan sendiri — itulah isi ADR Anda.

```sql
CREATE TABLE accounts (
    id           UUID PRIMARY KEY,
    name         TEXT NOT NULL,
    currency     CHAR(3) NOT NULL,
    account_type TEXT NOT NULL,   -- asset | liability | equity | revenue | expense
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transactions (
    id              UUID PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    reference       TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE entries (
    id             BIGSERIAL PRIMARY KEY,
    transaction_id UUID NOT NULL REFERENCES transactions(id),
    account_id     UUID NOT NULL REFERENCES accounts(id),
    amount         BIGINT NOT NULL,   -- satuan minor, bertanda
    currency       CHAR(3) NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_entries_account ON entries (account_id, created_at DESC);
CREATE INDEX idx_entries_txn     ON entries (transaction_id);

CREATE TABLE outbox (
    id           BIGSERIAL PRIMARY KEY,
    aggregate_id UUID NOT NULL,
    event_type   TEXT NOT NULL,
    payload      JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);

-- index parsial: hanya baris yang belum dipublikasikan
CREATE INDEX idx_outbox_pending ON outbox (id) WHERE published_at IS NULL;
```

Perhatikan index parsial pada `outbox`. Publisher Anda hanya akan menanyakan baris yang belum terkirim, dan tabel ini akan tumbuh besar. Detail kecil seperti ini yang membuat reviewer berhenti membaca sekilas.

---

## 4. Pertanyaan Terbuka — putuskan minggu ini

**a. Konvensi tanda.** Debit positif dan kredit negatif, atau sebaliknya? Tidak ada jawaban benar, tapi Anda harus memilih satu, mendokumentasikannya, dan konsisten selamanya. Dokumentasikan di ADR 1.

**b. Bagaimana menegakkan `SUM(amount) = 0` per transaksi?** Ini pertanyaan paling menarik di minggu ini. `CHECK` biasa tidak bisa melakukannya karena melibatkan banyak baris. Opsi Anda:
- Ditegakkan di lapisan aplikasi saja
- Trigger `CONSTRAINT ... DEFERRABLE INITIALLY DEFERRED` yang diperiksa saat commit
- Job rekonsiliasi yang memverifikasi berkala

Masing-masing punya konsekuensi berbeda soal performa dan jaminan. Pilih, dan tulis alasannya. Pewawancara akan menanyakan ini.

**c. Apakah entri lintas mata uang diizinkan dalam satu transaksi?** Saran: larang. Tegakkan di skema atau aplikasi, dan catat sebagai keputusan sadar.

**d. Bagaimana menjamin `entries` benar-benar append-only?** Konvensi tim saja tidak cukup. Pertimbangkan `REVOKE UPDATE, DELETE ON entries` untuk role aplikasi, atau trigger penolak. Menegakkan invarian di level database, bukan hanya di kode, adalah tanda kematangan.

**e. Apakah `transactions` butuh kolom `status`?** Pikirkan baik-baik. Kalau seluruh transaksi ditulis dalam satu transaksi database, hasilnya hanya commit atau rollback — tidak ada keadaan di antaranya. Status baru bermakna kalau ada alur asinkron multi-langkah. Menghapus kolom yang tidak perlu adalah keputusan desain yang layak ditulis.

---

## 5. CI Sejak Hari Pertama

`.github/workflows/ci.yml`:

```yaml
name: CI
on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      - name: Vet
        run: go vet ./...
      - name: Lint
        uses: golangci/golangci-lint-action@v6
      - name: Test
        run: go test -race -count=1 ./...
```

Perhatikan flag `-race`. Proyek ini berisi kode konkuren, dan race detector akan menemukan bug yang tidak akan pernah Anda temukan secara manual. Nyalakan sejak hari pertama, bukan setelah masalah muncul.

CI harus hijau di commit pertama, bahkan ketika belum ada yang diuji. Membiasakan diri dengan pipeline yang selalu hijau jauh lebih mudah daripada memperbaikinya belakangan.

---

## 6. README Awal

Isi minggu ini, sebelum kode apa pun:

```markdown
# Ledger

Layanan buku besar berpasangan (double-entry) dengan jaminan
idempotensi dan pengiriman webhook yang andal.

## Status
Proyek pembelajaran. Bukan sistem pembayaran produksi.

## Masalah yang Dipecahkan
[3–4 kalimat: kenapa saldo tidak boleh dihitung dengan UPDATE,
kenapa retry klien memaksa idempotensi, kenapa webhook tidak
boleh memblokir transaksi]

## Non-Goals
- Frontend
- Integrasi rail pembayaran nyata
- Konversi mata uang
- Manajemen pengguna di luar API key

## Arsitektur
[menyusul]
```

Menulis pernyataan masalah sebelum kode akan menjaga Anda tetap jujur soal cakupan. Setiap kali tergoda menambah fitur, baca ulang bagian Non-Goals.

---

## 7. Dua ADR Pertama

Format: **Konteks · Opsi yang dipertimbangkan · Keputusan · Konsekuensi.** Satu halaman, tidak lebih.

**ADR 001 — Double-entry, bukan kolom saldo.** Kenapa entri immutable mengalahkan `UPDATE accounts SET balance = balance - x`. Bahas jejak audit, kemampuan rekonstruksi keadaan, dan hilangnya seluruh kelas bug lost-update.

**ADR 002 — Uang sebagai BIGINT satuan minor.** Kenapa bukan `FLOAT` atau `DECIMAL`. Sertakan demonstrasi konkret: jalankan `0.1 + 0.2` di Go dan tempelkan hasilnya. Bahas juga kenapa `NUMERIC` benar tapi lebih lambat dan lebih merepotkan untuk aritmetika di kode.

Tulis ADR **saat keputusan diambil**, bukan direkonstruksi di Minggu 10. Yang ditulis dari ingatan selalu terbaca hambar dan kehilangan alternatif yang sempat Anda pertimbangkan.

---

## Checklist Minggu 1

- [ ] Repo publik dibuat, lisensi MIT dipasang
- [ ] `go mod init`, struktur direktori disiapkan
- [ ] `docker-compose.yml` dengan Postgres
- [ ] Migrasi pertama jalan, skema terbentuk
- [ ] `sqlc` terkonfigurasi
- [ ] CI hijau
- [ ] README dengan pernyataan masalah dan non-goals
- [ ] ADR 001 dan 002
- [ ] Lima pertanyaan terbuka di atas sudah diputuskan

---

## Satu Peringatan

Saya bisa menulis seluruh proyek ini untuk Anda dalam beberapa jam. **Jangan minta itu.**

Nilai proyek ini bukan pada kodenya — kode ledger sudah ada puluhan versi open source yang lebih baik. Nilainya ada pada kemampuan Anda menjelaskan setiap keputusan selama 20 menit tanpa gagap, di ruang wawancara, ketika seseorang menekan dengan pertanyaan yang tidak Anda siapkan.

Kemampuan itu hanya tumbuh dari Anda yang bergulat sendiri dengan masalahnya.

Yang produktif dari saya: mereview desain Anda, membedah kode yang sudah Anda tulis, menantang keputusan yang terlalu cepat Anda ambil, menjelaskan konsep yang belum jelas, dan membantu saat benar-benar buntu setelah Anda mencoba. Bukan menuliskan implementasinya.

Ini juga menjawab kekhawatiran Anda soal repo publik: yang membuat proyek ini milik Anda adalah proses berpikirnya, dan itu tidak bisa disalin siapa pun — termasuk oleh Anda sendiri dari saya.
