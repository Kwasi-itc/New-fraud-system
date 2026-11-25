# High-Load Aggregates Phase 0 Baseline Report

- **Generated at**: `2026-11-25T19:05:02.303668+00:00`
- **PostgreSQL**: `postgres:16` (isolated disposable container, `shared_buffers=512MB`, `work_mem=16MB`, `max_connections=300`)
- **Dataset**: `1401000` rows (`188 MB`), seeded in `63.8s`
- **In-place `ModePatch` churn**: `n_tup_upd=70292`, `n_tup_hot_upd=3`, `n_dead_tup=70290`
- **Target SLO Gate**: Aggregate `p99 <= 25 ms` at `200` concurrent evaluations on `m_hot_1m` (1M-row merchant, 7d window), `< 10%` `BatchIngest` regression.

## 1. EXPLAIN (ANALYZE, BUFFERS) Plans

| Index Mode | Merchant | Query | Plan Nodes | Exec (ms) | Heap Fetches | Shared Hit | Shared Read |
| :--- | :--- | :--- | :--- | ---: | ---: | ---: | ---: |
| `no_agg_index` | `m_hot_1k` | `count_1h` | Aggregate → Gather → Aggregate → Seq Scan | 155.202 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1k` | `sum_24h` | Aggregate → Gather → Aggregate → Seq Scan | 188.732 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1k` | `sum_7d` | Aggregate → Gather → Aggregate → Seq Scan | 166.363 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1k` | `avg_30d` | Aggregate → Gather → Aggregate → Seq Scan | 202.096 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1k` | `sum_7d_filtered` | Aggregate → Gather → Aggregate → Seq Scan | 366.424 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_100k` | `count_1h` | Aggregate → Gather → Aggregate → Seq Scan | 138.709 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_100k` | `sum_24h` | Aggregate → Gather → Aggregate → Seq Scan | 184.668 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_100k` | `sum_7d` | Aggregate → Gather → Aggregate → Seq Scan | 188.63 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_100k` | `avg_30d` | Aggregate → Gather → Aggregate → Seq Scan | 187.391 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_100k` | `sum_7d_filtered` | Aggregate → Gather → Aggregate → Seq Scan | 205.109 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1m` | `count_1h` | Aggregate → Gather → Aggregate → Seq Scan | 304.314 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1m` | `sum_24h` | Aggregate → Gather → Aggregate → Seq Scan | 247.97 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1m` | `sum_7d` | Aggregate → Gather → Aggregate → Seq Scan | 553.083 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1m` | `avg_30d` | Aggregate → Gather → Aggregate → Seq Scan | 438.131 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1m` | `sum_7d_filtered` | Aggregate → Gather → Aggregate → Seq Scan | 356.538 | 0 | 24237 | 0 |
| `current_pub_index` | `m_hot_1k` | `count_1h` | Aggregate → Index Scan | 0.299 | 0 | 10 | 2 |
| `current_pub_index` | `m_hot_1k` | `sum_24h` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 0.686 | 0 | 22 | 5 |
| `current_pub_index` | `m_hot_1k` | `sum_7d` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 1.343 | 0 | 27 | 0 |
| `current_pub_index` | `m_hot_1k` | `avg_30d` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 0.43 | 0 | 27 | 0 |
| `current_pub_index` | `m_hot_1k` | `sum_7d_filtered` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 0.828 | 0 | 27 | 0 |
| `current_pub_index` | `m_hot_100k` | `count_1h` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 13.435 | 0 | 65 | 17 |
| `current_pub_index` | `m_hot_100k` | `sum_24h` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 51.779 | 0 | 1503 | 408 |
| `current_pub_index` | `m_hot_100k` | `sum_7d` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 72.261 | 0 | 2147 | 67 |
| `current_pub_index` | `m_hot_100k` | `avg_30d` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 60.725 | 0 | 2214 | 0 |
| `current_pub_index` | `m_hot_100k` | `sum_7d_filtered` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 39.065 | 0 | 2214 | 0 |
| `current_pub_index` | `m_hot_1m` | `count_1h` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 2.91 | 0 | 123 | 27 |
| `current_pub_index` | `m_hot_1m` | `sum_24h` | Aggregate → Gather → Aggregate → Bitmap Heap Scan | 68.952 | 0 | 2993 | 588 |
| `current_pub_index` | `m_hot_1m` | `sum_7d` | Aggregate → Gather → Aggregate → Seq Scan | 372.057 | 0 | 24237 | 0 |
| `current_pub_index` | `m_hot_1m` | `avg_30d` | Aggregate → Gather → Aggregate → Seq Scan | 390.564 | 0 | 24237 | 0 |
| `current_pub_index` | `m_hot_1m` | `sum_7d_filtered` | Aggregate → Gather → Aggregate → Seq Scan | 357.407 | 0 | 24237 | 0 |

## 2. Concurrent Evaluation Latency (p50 / p95 / p99)

| Index Mode | Merchant | Query | Concurrency | Queries | TPS | p50 (ms) | p95 (ms) | p99 (ms) | PG CPU avg/peak (%) |
| :--- | :--- | :--- | ---: | ---: | ---: | ---: | ---: | ---: | :--- |
| `no_agg_index` | `m_hot_100k` | `sum_7d` | 1 | 37 | 7.37 | 125.58 | 226.68 | 231.88 | 286.3% / 286.8% |
| `no_agg_index` | `m_hot_100k` | `sum_7d` | 50 | 79 | 11.22 | 3668.64 | 6478.46 | 6568.39 | 1066.6% / 1253.6% |
| `no_agg_index` | `m_hot_1m` | `sum_7d` | 1 | 15 | 2.96 | 301.78 | 504.04 | 521.32 | 292.1% / 294.6% |
| `current_pub_index` | `m_hot_1k` | `sum_7d` | 1 | 4029 | 806.53 | 1.16 | 1.78 | 2.29 | 64.5% / 99.4% |
| `current_pub_index` | `m_hot_1k` | `sum_7d` | 50 | 22059 | 4448.37 | 7.3 | 21.98 | 33.19 | 726.6% / 880.2% |
| `current_pub_index` | `m_hot_1k` | `sum_7d` | 200 | 16626 | 3618.76 | 27.71 | 90.3 | 165.19 | 860.8% / 954.3% |
| `current_pub_index` | `m_hot_100k` | `sum_7d` | 1 | 166 | 33.14 | 26.18 | 55.92 | 70.0 | 73.7% / 100.1% |
| `current_pub_index` | `m_hot_100k` | `sum_7d` | 50 | 509 | 92.3 | 453.88 | 1216.41 | 1498.32 | 975.0% / 1247.5% |
| `current_pub_index` | `m_hot_100k` | `sum_7d` | 200 | 501 | 89.53 | 1709.85 | 5161.33 | 5505.02 | 895.1% / 1401.5% |
| `current_pub_index` | `m_hot_1m` | `count_1h` | 1 | 1819 | 364.29 | 1.95 | 5.58 | 7.73 | 102.8% / 106.0% |
| `current_pub_index` | `m_hot_1m` | `count_1h` | 50 | 6951 | 1389.96 | 26.89 | 83.75 | 125.6 | 574.5% / 899.4% |
| `current_pub_index` | `m_hot_1m` | `count_1h` | 200 | 8158 | 1685.84 | 79.95 | 244.63 | 344.11 | 802.4% / 1052.1% |
| `current_pub_index` | `m_hot_1m` | `sum_24h` | 1 | 73 | 14.49 | 66.18 | 100.06 | 120.55 | 233.4% / 236.2% |
| `current_pub_index` | `m_hot_1m` | `sum_24h` | 50 | 269 | 47.46 | 883.37 | 1945.54 | 2223.5 | 802.6% / 1618.5% |
| `current_pub_index` | `m_hot_1m` | `sum_24h` | 200 | 238 | 30.67 | 5871.42 | 7590.11 | 7672.55 | 875.2% / 1643.1% |
| `current_pub_index` | `m_hot_1m` | `sum_7d` | 1 | 11 | 2.07 | 470.96 | 614.76 | 626.5 | 259.5% / 294.0% |
| `current_pub_index` | `m_hot_1m` | `sum_7d` | 50 | 53 | 4.43 | 9423.98 | 11777.7 | 11898.79 | 1042.4% / 1427.3% |
| `current_pub_index` | `m_hot_1m` | `sum_7d` | 200 | 200 | 4.46 | 39001.45 | 44414.83 | 44640.16 | 1375.7% / 1739.2% |
| `current_pub_index` | `m_hot_1m` | `avg_30d` | 1 | 8 | 1.51 | 651.9 | 863.7 | 880.75 | 190.2% / 307.8% |
| `current_pub_index` | `m_hot_1m` | `avg_30d` | 50 | 53 | 3.95 | 11456.8 | 13029.42 | 13409.2 | 1166.7% / 1363.5% |
| `current_pub_index` | `m_hot_1m` | `avg_30d` | 200 | 200 | 4.27 | 40692.9 | 46121.55 | 46616.32 | 1407.1% / 1832.6% |
| `current_pub_index` | `m_hot_1m` | `sum_7d_filtered` | 1 | 6 | 1.2 | 858.23 | 942.6 | 943.24 | 177.1% / 317.0% |
| `current_pub_index` | `m_hot_1m` | `sum_7d_filtered` | 50 | 51 | 3.43 | 11504.3 | 14060.15 | 14522.9 | 936.0% / 1479.9% |
| `current_pub_index` | `m_hot_1m` | `sum_7d_filtered` | 200 | 200 | 3.84 | 44287.75 | 51006.15 | 51441.75 | 1324.6% / 1856.8% |

## 3. BatchIngest (500-row batches) Throughput

| Label | Batches | Total Rows | Elapsed (s) | Throughput (rows/s) | PG CPU avg (%) |
| :--- | ---: | ---: | ---: | ---: | ---: |
| `no_agg_index` | 20 | 10000 | 6.646 | 1504.8 | 54.1% |
| `current_pub_index` | 20 | 10000 | 8.655 | 1155.4 | 58.7% |
