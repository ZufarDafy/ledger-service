// Package ledger berisi logika domain buku besar berpasangan:
// aturan validasi transaksi, invarian jumlah-nol, dan perhitungan saldo.
//
// Paket ini sengaja tidak bergantung pada database, HTTP, maupun gRPC.
// Seluruh isinya harus bisa diuji tanpa Docker dan tanpa Postgres.
package ledger
