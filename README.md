# Pgpool-II Exporter

Prometheus exporter for [Pgpool-II](https://pgpool.net) metrics.

Supported Pgpool-II 3.6 and later.


## Building and running

### Build
```
$ make
```

### Running

Running using an environment variable:
```
$ export DATA_SOURCE_NAME="postgresql://user:password@hostname:port/dbname"
$ ./pgpool2_exporter <flags>
```
    
To see all available configuration flags:
```
$ ./pgpool2_exporter --help
```
    
 ### Flags

* `version`
  Print version information.
  
* `web.listen-address`
  Address on which to expose metrics and web interface. (default ":9719").

* `web.telemetry-path`
  Path under which to expose metrics. (default "/metrics")
  
## Metrics

name | Description
:---|:---
pgpool2_frontend_total | Number of total child processes
pgpool2_frontend_used | Number of used child processes
pgpool2_pool_nodes_status | Backend node Status (1 for up or waiting, 0 for down or unused)
pgpool2_pool_nodes_replication_delay | Replication delay
pgpool2_pool_nodes_select_cnt | SELECT query counts issued to each backend
pgpool2_pool_cache_cache_hit_ratio | Query cache hit ratio
pgpool2_pool_cache_num_cache_entries | Number of used cache entries
pgpool2_pool_cache_num_hash_entries | Number of total hash entries
pgpool2_pool_cache_used_hash_entries | Number of used hash entries
pgpool2_pool_backend_stats_select_cnt | SELECT statement counts issued to each backend
pgpool2_pool_backend_stats_insert_cnt | INSERT statement counts issued to each backend
pgpool2_pool_backend_stats_update_cnt | UPDATE statement counts issued to each backend
pgpool2_pool_backend_stats_delete_cnt | DELETE statement counts issued to each backend
pgpool2_pool_backend_stats_ddl_cnt | DDL statement counts issued to each backend
pgpool2_pool_backend_stats_other_cnt | other statement counts issued to each backend
pgpool2_pool_backend_stats_panic_cnt | Panic message counts returned from backend
pgpool2_pool_backend_stats_fatal_cnt | Fatal message counts returned from backend
pgpool2_pool_backend_stats_error_cnt | Error message counts returned from backend
pgpool2_pool_health_check_stats_total_count | Number of health check count in total
pgpool2_pool_health_check_stats_success_count | Number of successful health check count in total
pgpool2_pool_health_check_stats_fail_count | Number of failed health check count in total
pgpool2_pool_health_check_stats_skip_count | Number of skipped health check count in total
pgpool2_pool_health_check_stats_retry_count | Number of retried health check count in total
pgpool2_pool_health_check_stats_average_retry_count | Number of average retried health check count in a health check session
pgpool2_pool_health_check_stats_max_retry_count | Number of maximum retried health check count in a health check session
pgpool2_pool_health_check_stats_max_duration | Maximum health check duration in Millie seconds
pgpool2_pool_health_check_stats_min_duration | Minimum health check duration in Millie seconds
pgpool2_pool_health_check_stats_average_duration | Average health check duration in Millie seconds
