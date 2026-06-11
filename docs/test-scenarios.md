# Сценарии тестирования

Документ описывает тесты, которые использовались для сравнения трех конфигураций приложения:

1. `baseline` - приложение без обработчика `SIGTERM` и без учета операций;
2. `server-shutdown` - приложение с обработчиком `SIGTERM` и вызовом `http.Server.Shutdown`, но без readiness/draining и без прикладного журнала ресурсов;
3. `module` - приложение с разработанным модулем корректного завершения.

## Нагрузочная модель

В каждом запуске отправляется 50 HTTP-запросов методом `GET` к маршруту `/work`. Запросы стартуют с интервалом 100 мс, но каждый запрос выполняется в отдельной goroutine, поэтому нагрузка является параллельной. Остановка пода инициируется через 3 секунды после начала нагрузки. К этому моменту часть коротких запросов уже завершена, часть средних и длительных запросов еще находится в обработке, а оставшиеся запросы продолжают стартовать.

Распределение задержек:

- 35 коротких запросов: 100-300 мс;
- 10 средних запросов: 2-5 с;
- 5 длительных запросов: 12-18 с;
- отдельный timeout-сценарий: 1 запрос с задержкой 40 с при `SHUTDOWN_TIMEOUT_SECONDS=25`.

Такое распределение моделирует неоднородность времени обработки и наличие длинных операций. Оно не претендует на точное воспроизведение промышленного профиля трафика, но позволяет проверить ключевое свойство модуля: ожидание зарегистрированных операций в момент остановки пода.

## Запуск нагрузки

```bash
go run ./cmd/load-client --target http://localhost:8080/work --interval 100ms
```

Для Kubernetes можно выполнить port-forward:

```bash
kubectl port-forward svc/graceful-app 8080:80
```

## Основные сценарии

| Сценарий | Команда | Ожидаемый результат |
|---|---|---|
| Удаление пода | `kubectl delete pod <pod>` | Зарегистрированные операции завершаются в пределах timeout |
| Rolling update, 2 реплики | `kubectl rollout restart deploy/graceful-app` | Новая нагрузка уходит на готовую реплику |
| Rolling update, 1 реплика | `kubectl scale deploy/graceful-app --replicas=1` и restart | Уже начатые операции сохраняются, непрерывность новых запросов не гарантируется |
| Scale down | `kubectl scale deploy/graceful-app --replicas=1` | Завершаемый под переходит в `draining` |
| Timeout | `GET /work?delay=40s&op=long-01` | В журнале появляется `shutdown_timeout` с описанием операции |
| Асинхронная задача | `GET /work?delay=10s&op=async-01&async=true` | В журнале появляются `async_started`, `async_finished` или `shutdown_timeout` |
| Закрытие ресурсов | Остановка пода с модулем | В журнале фиксируются `resource_close` для `demo_db` и `demo_queue_client` |

## Примеры журналов

Успешное завершение:

```json
{"event":"shutdown_requested","source":"preStop","active_ops":4}
{"event":"readiness_changed","state":"draining","active_ops":4}
{"event":"active_ops_wait","active_ops":2}
{"event":"active_ops_done","active_ops":0,"result":"all_registered_operations_completed"}
{"event":"resource_close","resource":"demo_db","result":"ok"}
{"event":"resource_close","resource":"demo_queue_client","result":"ok"}
{"event":"shutdown_complete","result":"graceful"}
```

Превышение timeout:

```json
{"event":"shutdown_timeout","active_ops":1,"timeout_ms":25000,"operations":[{"op_id":"long-01","method":"GET","path":"/work","remote_ip":"10.244.0.x","elapsed_ms":25000}],"result":"forced_by_app_policy"}
```
