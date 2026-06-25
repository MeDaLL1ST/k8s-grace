# k8s-grace

`k8s-grace` - учебный прикладной модуль для небольших HTTP-приложений на Go, развертываемых в Kubernetes. Модуль согласует readiness-пробу, `preStop`, `SIGTERM`, учет активных HTTP-запросов и асинхронных операций, закрытие ресурсов и структурированный журнал завершения.

## Назначение

Kubernetes может инициировать остановку пода, вызвать `preStop`, отправить процессу `SIGTERM` и ограничить время ожидания параметром `terminationGracePeriodSeconds`. Однако Kubernetes не знает внутреннее состояние приложения: сколько запросов выполняется, какие фоновые задачи запущены, какие соединения с внешними системами нужно закрыть и какие операции не успели завершиться. Этот модуль добавляет прикладный уровень завершения для Go-сервиса на `net/http`.

## Возможности

- модель состояний `running`, `draining`, `stopping`, `stopped`;
- конфигурируемые маршруты readiness и shutdown;
- поддержка пользовательских readiness-check, например проверки БД или кэша;
- middleware для учета активных HTTP-запросов;
- учет фоновых задач через `TrackAsync`;
- реестр функций закрытия ресурсов через `RegisterCloser`;
- configurable timeout через `SHUTDOWN_TIMEOUT_SECONDS`;
- JSON-журнал в stdout контейнера;
- три режима демонстрационного приложения: `baseline`, `server-shutdown`, `module`.

## Структура репозитория

```text
k8s-grace/
├── README.md
├── LICENSE
├── Dockerfile
├── go.mod
├── cmd/
│   ├── demo-app/        # демонстрационный HTTP-сервис
│   └── load-client/     # клиент для экспериментальной нагрузки
├── pkg/
│   └── shutdown/        # код модуля корректного завершения
├── k8s/                 # манифесты Kubernetes для трех конфигураций
└── docs/                # сценарии тестирования, результаты, материалы README
```

Скриншоты структуры репозитория и фрагмента README для вставки в ВКР находятся в каталоге `docs/assets`.

## Быстрый запуск локально

```bash
go run ./cmd/demo-app
```

Проверка готовности:

```bash
curl -i http://localhost:8080/ready
```

Тестовый запрос:

```bash
curl "http://localhost:8080/work?delay=5s&op=demo-1"
```

Перевод в режим завершения:

```bash
curl -i http://localhost:8080/shutdown
```

Асинхронная операция:

```bash
curl "http://localhost:8080/work?delay=10s&op=async-1&async=true"
```

## Подключение к новому Go-приложению

```go
cfg := shutdown.LoadConfig()
manager := shutdown.New(cfg)

mux := http.NewServeMux()
mux.HandleFunc(cfg.ReadyPath, manager.ReadyHandler())
mux.HandleFunc(cfg.ShutdownPath, manager.ShutdownHandler())
mux.Handle("/api/", manager.Middleware(apiHandler))

manager.RegisterReadyCheck("database", func(ctx context.Context) error {
    return db.PingContext(ctx)
})
manager.RegisterCloser("database", func(ctx context.Context) error {
    return db.Close()
})

server := &http.Server{Addr: ":8080", Handler: mux}
shutdownDone := manager.RunSignalHandler(context.Background(), server)
```

Если обработчик запускает фоновую задачу, ее нужно регистрировать через `TrackAsync`:

```go
manager.TrackAsync(r.Context(), "job-42", func(ctx context.Context) {
    runBackgroundJob(ctx)
})
```

Операции, запущенные напрямую через `go func(){...}()` без регистрации в модуле, не учитываются при остановке. Это ограничение важно указывать в документации сервиса.

## Режимы демонстрационного приложения

| Режим | Переменная `APP_MODE` | Назначение |
|---|---|---|
| Базовая конфигурация | `baseline` | Нет обработчика SIGTERM и прикладного учета операций |
| Стандартный обработчик | `server-shutdown` | Используется только `http.Server.Shutdown` |
| Модуль корректного завершения | `module` | Используются readiness, preStop, учет операций, ресурсы и журнал |

Пример запуска:

```bash
APP_MODE=server-shutdown go run ./cmd/demo-app
```

## Переменные окружения

| Переменная | Назначение | Пример |
|---|---|---|
| `APP_MODE` | Режим демонстрации: `baseline`, `server-shutdown`, `module` | `module` |
| `SHUTDOWN_TIMEOUT_SECONDS` | Максимальное ожидание активных операций | `25` |
| `LOG_FORMAT` | Формат журнала: `json` или `text` | `json` |
| `READY_PATH` | Маршрут readiness-пробы | `/ready` |
| `SHUTDOWN_PATH` | Маршрут preStop | `/shutdown` |
| `SHUTDOWN_TOKEN` | Необязательный токен для защиты `/shutdown` | `secret` |

## Развертывание в Kubernetes

```bash
minikube start
eval $(minikube docker-env)
docker build -t graceful-app:1.0 .
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/deployment.yaml
kubectl get pods
```

Проверка журналов:

```bash
kubectl logs deploy/graceful-app
```

## Нагрузочный клиент

Клиент запускает HTTP-запросы с контролируемым случайным распределением длительности. Фиксированными остаются доли типов запросов и диапазоны задержек, а конкретная задержка каждого запроса выбирается случайно внутри своего диапазона. При значении по умолчанию `--count 50` формируется 35 коротких, 10 средних и 5 длительных запросов. Каждый запрос выполняется в отдельной goroutine, поэтому операции частично перекрываются по времени.

```bash
go run ./cmd/load-client --target http://localhost:8080/work --interval 100ms --mode mixed --count 50
```

Для воспроизводимости можно указать seed:

```bash
go run ./cmd/load-client --target http://localhost:8080/work --interval 100ms --mode mixed --count 50 --seed 42
```

Негативный сценарий с запросом на 60 секунд при `SHUTDOWN_TIMEOUT_SECONDS=25`:

```bash
go run ./cmd/load-client --target http://localhost:8080/work --mode timeout --timeout-delay 60s
```

Выбор 50 запусков в эксперименте связан с тем, что 10-20 повторов дают слишком сильную зависимость от случайного порядка запросов и момента остановки пода. 50 повторов являются компромиссом: они сглаживают случайные отклонения, но остаются выполнимыми на локальном стенде Minikube.

## Безопасность `/shutdown`

Маршрут `/shutdown` предназначен для внутреннего вызова Kubernetes через `preStop`. В промышленной среде его не следует публиковать наружу через Ingress. Возможные меры защиты:

- отдельный внутренний порт для служебных маршрутов;
- NetworkPolicy, разрешающая доступ только из нужного контура;
- непубличный `SHUTDOWN_PATH`;
- токен в заголовке `X-Shutdown-Token`, если используется `SHUTDOWN_TOKEN`.

## Пример журнала

```json
{"event":"shutdown_requested","source":"preStop","active_ops":4}
{"event":"readiness_changed","state":"draining","active_ops":4}
{"event":"active_ops_wait","active_ops":2}
{"event":"active_ops_done","active_ops":0,"result":"all_registered_operations_completed"}
{"event":"resource_close","resource":"demo_db","result":"ok"}
{"event":"shutdown_complete","result":"graceful"}
```

## Ограничения

- модуль не защищает от аварийного отключения узла;
- модуль не гарантирует завершение при `kubectl delete pod --force --grace-period=0`;
- асинхронные задачи учитываются только при регистрации через `TrackAsync`;
- бизнес-операции не завершаются модулем принудительно, модуль только ожидает их завершения в пределах timeout;
- точный набор полей журнала нужно согласовать с требованиями безопасности и обработки данных.

## Лицензия

Проект распространяется по лицензии MIT. См. файл `LICENSE`.
