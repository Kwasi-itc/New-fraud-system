# High-Load Aggregates Phase 1 Covering Index Gate Report

- **Generated at**: `2026-11-25T19:56:51.317694+00:00`
- **PostgreSQL**: `postgres:16` (isolated disposable container, `shared_buffers=512MB`, `work_mem=16MB`, `max_connections=300`)
- **Dataset**: `1401000` rows (`188 MB`), seeded in `57.58s`
- **In-place `ModePatch` churn**: `n_tup_upd=70292`, `n_tup_hot_upd=3`, `n_dead_tup=70291`
- **Target SLO Gate**: Aggregate `p99 <= 25 ms` at `200` concurrent evaluations on `m_hot_1m` (1M-row merchant, 7d window), `< 10%` `BatchIngest` regression.

## 1. EXPLAIN (ANALYZE, BUFFERS) Plans

| Index Mode | Merchant | Query | Plan Nodes | Exec (ms) | Heap Fetches | Shared Hit | Shared Read |
| :--- | :--- | :--- | :--- | ---: | ---: | ---: | ---: |
| `no_agg_index` | `m_hot_1k` | `count_1h` | Aggregate → Gather → Aggregate → Seq Scan | 232.215 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1k` | `sum_24h` | Aggregate → Gather → Aggregate → Seq Scan | 109.212 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1k` | `sum_7d` | Aggregate → Gather → Aggregate → Seq Scan | 235.221 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1k` | `avg_30d` | Aggregate → Gather → Aggregate → Seq Scan | 108.705 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1k` | `sum_7d_filtered` | Aggregate → Gather → Aggregate → Seq Scan | 125.983 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_100k` | `count_1h` | Aggregate → Gather → Aggregate → Seq Scan | 119.888 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_100k` | `sum_24h` | Aggregate → Gather → Aggregate → Seq Scan | 150.425 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_100k` | `sum_7d` | Aggregate → Gather → Aggregate → Seq Scan | 115.7 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_100k` | `avg_30d` | Aggregate → Gather → Aggregate → Seq Scan | 93.147 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_100k` | `sum_7d_filtered` | Aggregate → Gather → Aggregate → Seq Scan | 93.759 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1m` | `count_1h` | Aggregate → Gather → Aggregate → Seq Scan | 106.643 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1m` | `sum_24h` | Aggregate → Gather → Aggregate → Seq Scan | 117.486 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1m` | `sum_7d` | Aggregate → Gather → Aggregate → Seq Scan | 196.834 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1m` | `avg_30d` | Aggregate → Gather → Aggregate → Seq Scan | 287.114 | 0 | 24237 | 0 |
| `no_agg_index` | `m_hot_1m` | `sum_7d_filtered` | Aggregate → Gather → Aggregate → Seq Scan | 409.074 | 0 | 24237 | 0 |
| `current_pub_index` | `m_hot_1k` | `count_1h` | Aggregate → Index Scan | 0.241 | 0 | 10 | 2 |
| `current_pub_index` | `m_hot_1k` | `sum_24h` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 0.586 | 0 | 22 | 5 |
| `current_pub_index` | `m_hot_1k` | `sum_7d` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 1.406 | 0 | 27 | 0 |
| `current_pub_index` | `m_hot_1k` | `avg_30d` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 0.623 | 0 | 27 | 0 |
| `current_pub_index` | `m_hot_1k` | `sum_7d_filtered` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 0.524 | 0 | 27 | 0 |
| `current_pub_index` | `m_hot_100k` | `count_1h` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 1.77 | 0 | 65 | 17 |
| `current_pub_index` | `m_hot_100k` | `sum_24h` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 33.652 | 0 | 1504 | 408 |
| `current_pub_index` | `m_hot_100k` | `sum_7d` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 32.763 | 0 | 2147 | 67 |
| `current_pub_index` | `m_hot_100k` | `avg_30d` | Aggregate → Gather → Aggregate → Bitmap Heap Scan | 26.775 | 0 | 2214 | 0 |
| `current_pub_index` | `m_hot_100k` | `sum_7d_filtered` | Aggregate → Gather → Aggregate → Bitmap Heap Scan | 37.493 | 0 | 2214 | 0 |
| `current_pub_index` | `m_hot_1m` | `count_1h` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 4.044 | 0 | 124 | 27 |
| `current_pub_index` | `m_hot_1m` | `sum_24h` | Aggregate → Gather → Aggregate → Bitmap Heap Scan | 43.841 | 0 | 2994 | 588 |
| `current_pub_index` | `m_hot_1m` | `sum_7d` | Aggregate → Gather → Aggregate → Seq Scan | 226.452 | 0 | 24237 | 0 |
| `current_pub_index` | `m_hot_1m` | `avg_30d` | Aggregate → Gather → Aggregate → Seq Scan | 259.224 | 0 | 24237 | 0 |
| `current_pub_index` | `m_hot_1m` | `sum_7d_filtered` | Aggregate → Gather → Aggregate → Seq Scan | 241.122 | 0 | 24237 | 0 |
| `phase1_covering_index` | `m_hot_1k` | `count_1h` | Aggregate → Index Scan | 0.404 | 0 | 10 | 0 |
| `phase1_covering_index` | `m_hot_1k` | `sum_24h` | Aggregate → Index Only Scan | 0.514 | 0 | 12 | 0 |
| `phase1_covering_index` | `m_hot_1k` | `sum_7d` | Aggregate → Index Only Scan | 0.846 | 0 | 12 | 0 |
| `phase1_covering_index` | `m_hot_1k` | `avg_30d` | Aggregate → Index Only Scan | 0.612 | 0 | 12 | 0 |
| `phase1_covering_index` | `m_hot_1k` | `sum_7d_filtered` | Aggregate → Index Only Scan | 1.109 | 0 | 12 | 0 |
| `phase1_covering_index` | `m_hot_100k` | `count_1h` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 1.371 | 0 | 76 | 0 |
| `phase1_covering_index` | `m_hot_100k` | `sum_24h` | Aggregate → Index Only Scan | 23.813 | 115 | 709 | 0 |
| `phase1_covering_index` | `m_hot_100k` | `sum_7d` | Aggregate → Index Only Scan | 22.692 | 170 | 824 | 0 |
| `phase1_covering_index` | `m_hot_100k` | `avg_30d` | Aggregate → Index Only Scan | 24.259 | 170 | 824 | 0 |
| `phase1_covering_index` | `m_hot_100k` | `sum_7d_filtered` | Aggregate → Index Only Scan | 22.614 | 170 | 824 | 0 |
| `phase1_covering_index` | `m_hot_1m` | `count_1h` | Aggregate → Bitmap Heap Scan → Bitmap Index Scan | 2.489 | 0 | 140 | 0 |
| `phase1_covering_index` | `m_hot_1m` | `sum_24h` | Aggregate → Gather → Aggregate → Index Only Scan | 39.341 | 285 | 1422 | 0 |
| `phase1_covering_index` | `m_hot_1m` | `sum_7d` | Aggregate → Gather → Aggregate → Seq Scan | 210.241 | 0 | 24237 | 0 |
| `phase1_covering_index` | `m_hot_1m` | `avg_30d` | Aggregate → Gather → Aggregate → Seq Scan | 308.778 | 0 | 24237 | 0 |
| `phase1_covering_index` | `m_hot_1m` | `sum_7d_filtered` | Aggregate → Gather → Aggregate → Seq Scan | 255.809 | 0 | 24237 | 0 |

## 2. Concurrent Evaluation Latency (p50 / p95 / p99)

| Index Mode | Merchant | Query | Concurrency | Queries | TPS | p50 (ms) | p95 (ms) | p99 (ms) | PG CPU avg/peak (%) |
| :--- | :--- | :--- | ---: | ---: | ---: | ---: | ---: | ---: | :--- |
| `no_agg_index` | `m_hot_100k` | `sum_7d` | 1 | 37 | 7.23 | 121.73 | 213.43 | 214.21 | 228.5% / 289.5% |
| `no_agg_index` | `m_hot_100k` | `sum_7d` | 50 | 111 | 17.71 | 2176.63 | 5008.83 | 5564.61 | 1247.8% / 1441.9% |
| `no_agg_index` | `m_hot_1m` | `sum_7d` | 1 | 22 | 4.24 | 202.61 | 351.9 | 374.46 | 196.0% / 296.4% |
| `current_pub_index` | `m_hot_1k` | `sum_7d` | 1 | 4799 | 960.43 | 0.94 | 1.6 | 2.94 | 66.4% / 100.2% |
| `current_pub_index` | `m_hot_1k` | `sum_7d` | 50 | 23932 | 4847.17 | 6.27 | 22.19 | 33.94 | 471.3% / 782.9% |
| `current_pub_index` | `m_hot_1k` | `sum_7d` | 200 | 23964 | 5305.26 | 18.95 | 59.67 | 130.52 | 589.5% / 888.3% |
| `current_pub_index` | `m_hot_100k` | `sum_7d` | 1 | 152 | 30.39 | 28.02 | 67.9 | 81.67 | 100.1% / 100.7% |
| `current_pub_index` | `m_hot_100k` | `sum_7d` | 50 | 730 | 142.75 | 286.74 | 800.36 | 1167.62 | 835.5% / 1259.9% |
| `current_pub_index` | `m_hot_100k` | `sum_7d` | 200 | 775 | 140.71 | 1084.13 | 3374.25 | 4452.23 | 1242.1% / 1373.8% |
| `current_pub_index` | `m_hot_1m` | `count_1h` | 1 | 2474 | 495.23 | 1.75 | 3.43 | 5.04 | 98.5% / 98.8% |
| `current_pub_index` | `m_hot_1m` | `count_1h` | 50 | 10963 | 2194.91 | 16.28 | 56.76 | 88.81 | 652.5% / 1052.4% |
| `current_pub_index` | `m_hot_1m` | `count_1h` | 200 | 9552 | 1949.73 | 70.7 | 215.47 | 303.07 | 638.4% / 968.0% |
| `current_pub_index` | `m_hot_1m` | `sum_24h` | 1 | 137 | 27.37 | 30.11 | 76.31 | 90.06 | 215.1% / 233.5% |
| `current_pub_index` | `m_hot_1m` | `sum_24h` | 50 | 457 | 85.56 | 469.73 | 1255.68 | 1665.33 | 1285.4% / 1457.3% |
| `current_pub_index` | `m_hot_1m` | `sum_24h` | 200 | 451 | 73.48 | 2068.13 | 5486.52 | 5799.02 | 1260.6% / 1582.4% |
| `current_pub_index` | `m_hot_1m` | `sum_7d` | 1 | 18 | 3.44 | 276.86 | 408.87 | 416.9 | 204.6% / 287.9% |
| `current_pub_index` | `m_hot_1m` | `sum_7d` | 50 | 62 | 8.6 | 5708.67 | 6947.13 | 7084.32 | 1378.5% / 1663.3% |
| `current_pub_index` | `m_hot_1m` | `sum_7d` | 200 | 201 | 8.6 | 20191.4 | 23026.9 | 23181.1 | 1190.5% / 1558.2% |
| `current_pub_index` | `m_hot_1m` | `avg_30d` | 1 | 20 | 3.91 | 267.98 | 302.75 | 351.65 | 193.3% / 290.1% |
| `current_pub_index` | `m_hot_1m` | `avg_30d` | 50 | 60 | 8.19 | 6140.32 | 6982.01 | 6989.51 | 1517.0% / 1643.0% |
| `current_pub_index` | `m_hot_1m` | `avg_30d` | 200 | 200 | 7.57 | 22681.75 | 26098.36 | 26286.52 | 1225.6% / 1451.3% |
| `current_pub_index` | `m_hot_1m` | `sum_7d_filtered` | 1 | 18 | 3.4 | 260.29 | 462.41 | 465.24 | 290.6% / 304.2% |
| `current_pub_index` | `m_hot_1m` | `sum_7d_filtered` | 50 | 60 | 5.41 | 8510.67 | 11043.06 | 11095.77 | 1025.8% / 1624.5% |
| `current_pub_index` | `m_hot_1m` | `sum_7d_filtered` | 200 | 200 | 4.99 | 34584.5 | 39391.54 | 39672.86 | 1445.0% / 1770.5% |
| `phase1_covering_index` | `m_hot_1k` | `sum_7d` | 1 | 3988 | 798.76 | 1.08 | 1.98 | 4.16 | 64.3% / 99.8% |
| `phase1_covering_index` | `m_hot_1k` | `sum_7d` | 50 | 22893 | 4638.16 | 6.29 | 21.53 | 32.1 | 666.1% / 769.4% |
| `phase1_covering_index` | `m_hot_1k` | `sum_7d` | 200 | 21381 | 4480.51 | 20.58 | 68.46 | 107.82 | 527.6% / 791.6% |
| `phase1_covering_index` | `m_hot_100k` | `sum_7d` | 1 | 198 | 39.52 | 21.97 | 45.04 | 64.3 | 66.0% / 99.6% |
| `phase1_covering_index` | `m_hot_100k` | `sum_7d` | 50 | 870 | 170.26 | 227.74 | 679.54 | 862.76 | 827.4% / 1278.8% |
| `phase1_covering_index` | `m_hot_100k` | `sum_7d` | 200 | 1109 | 198.33 | 906.77 | 2026.58 | 2364.56 | 1092.9% / 1541.7% |
| `phase1_covering_index` | `m_hot_1m` | `count_1h` | 1 | 2232 | 446.67 | 1.86 | 4.16 | 7.13 | 99.0% / 99.4% |
| `phase1_covering_index` | `m_hot_1m` | `count_1h` | 50 | 6784 | 1369.1 | 24.95 | 94.52 | 137.84 | 502.3% / 945.8% |
| `phase1_covering_index` | `m_hot_1m` | `count_1h` | 200 | 7634 | 1590.95 | 79.86 | 299.49 | 451.2 | 851.4% / 1038.5% |
| `phase1_covering_index` | `m_hot_1m` | `sum_24h` | 1 | 90 | 17.99 | 52.06 | 94.61 | 115.16 | 228.1% / 238.4% |
| `phase1_covering_index` | `m_hot_1m` | `sum_24h` | 50 | 461 | 88.31 | 420.41 | 1356.6 | 2045.7 | 837.2% / 1345.8% |
| `phase1_covering_index` | `m_hot_1m` | `sum_24h` | 200 | 561 | 93.73 | 1514.0 | 4368.65 | 5419.42 | 1098.0% / 1251.3% |
| `phase1_covering_index` | `m_hot_1m` | `sum_7d` | 1 | 17 | 3.08 | 274.24 | 582.51 | 692.79 | 225.4% / 287.6% |
| `phase1_covering_index` | `m_hot_1m` | `sum_7d` | 50 | 67 | 8.34 | 5037.91 | 7472.92 | 7658.49 | 1137.2% / 1570.1% |
| `phase1_covering_index` | `m_hot_1m` | `sum_7d` | 200 | 200 | 5.43 | 31361.5 | 36424.34 | 36644.58 | 1311.5% / 1762.8% |
| `phase1_covering_index` | `m_hot_1m` | `avg_30d` | 1 | 13 | 2.49 | 400.04 | 606.18 | 620.64 | 247.1% / 300.5% |
| `phase1_covering_index` | `m_hot_1m` | `avg_30d` | 50 | 55 | 4.93 | 9388.32 | 10721.29 | 10808.99 | 1212.4% / 1628.1% |
| `phase1_covering_index` | `m_hot_1m` | `avg_30d` | 200 | 201 | 5.23 | 32481.1 | 37903.5 | 38254.3 | 1347.2% / 1596.1% |
| `phase1_covering_index` | `m_hot_1m` | `sum_7d_filtered` | 1 | 13 | 2.52 | 316.57 | 707.24 | 730.2 | 265.7% / 298.1% |
| `phase1_covering_index` | `m_hot_1m` | `sum_7d_filtered` | 50 | 53 | 4.8 | 9526.32 | 10940.24 | 10996.7 | 1314.8% / 1506.7% |
| `phase1_covering_index` | `m_hot_1m` | `sum_7d_filtered` | 200 | 200 | 5.24 | 32360.9 | 37613.24 | 37892.46 | 1297.7% / 1546.5% |

## 3. BatchIngest (500-row batches) Throughput

| Label | Batches | Total Rows | Elapsed (s) | Throughput (rows/s) | PG CPU avg (%) |
| :--- | ---: | ---: | ---: | ---: | ---: |
| `no_agg_index` | 20 | 10000 | 5.817 | 1719.1 | 38.4% |
| `current_pub_index` | 20 | 10000 | 7.178 | 1393.2 | 66.8% |
| `phase1_covering_index` | 20 | 10000 | 14.741 | 678.4 | 66.3% |
