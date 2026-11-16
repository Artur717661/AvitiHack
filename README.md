# AI-buddy

Команда **Shemelis AI**.

Помощник для стажёра, которому нужен доступ к вики команды. Стажёр спрашивает о порядке доступа, готовит заявку в чате и подтверждает отправку на отдельной странице. Тестовый сервис регистрирует заявку. Чат и страница показывают её номер, статус и следующий шаг.

[Презентация](AI-buddy.pdf) · [Видеодемо](demo.mp4) · [Спецификация сценария](docs/sdd.md)

## Быстрый запуск

Нужны Go 1.27.4, Node.js 24 и pnpm 11.25.0. Если pnpm ещё не установлен, выполни `npm install -g pnpm@11.25.0`. Остальные версии зафиксированы в Go-модуле, lock-файле фронтенда и [зависимостях ML-сервиса](requirements-ml.txt).

Из корня репозитория или распакованного ZIP:

```bash
pnpm --dir web install --frozen-lockfile
pnpm --dir web build
go run .
```

Открой [http://127.0.0.1:8080](http://127.0.0.1:8080). Go раздаёт сайт и API с одного адреса. При первом запуске создаётся `buddy.sqlite3`. Встроенный классификатор работает локально. Для повторного прогона с нуля укажи новый файл через `BUDDY_SQLITE`.

С PostgreSQL 17.11 можно запустить через Docker и Compose v2:

```bash
docker compose up --build -d
```

Сайт доступен по тому же адресу. Compose хранит заявки в томе PostgreSQL. Для запуска с уже работающей БД укажи её адрес в `DATABASE_URL`. Примеры переменных есть в [.env.example](.env.example).

## Сценарий для проверки

| Логин | Пароль | Проверка |
|---|---|---|
| `demo-intern-1` | `alpha-demo-1` | Основная учётка, уже заполнена на странице входа |
| `demo-intern-2` | `beta-demo-2` | У стажёра Beta нет доступа к сценарию Alpha |
| `demo-intern-3` | `alpha-demo-3` | Другой стажёр Alpha может создать свою заявку |

1. Войди в Alpha и спроси «Как получить доступ к вики?». Под ответом открой инструкцию команды.
2. Попроси подготовить заявку, введи причину и создай черновик. Открой его по ссылке из чата.
3. Нажми «Отправить заявку». Появятся номер `REQ-…`, статус «На рассмотрении» и вводная, которую можно изучить в ожидании согласования.
4. Обнови страницу и спроси в чате «Что с моей заявкой?». Номер должен совпасть.
5. Открой ссылку на этот черновик под третьей учёткой. Сервер откажет в чтении чужой заявки. Под второй учёткой сервер также откажет в доступе к инструкциям Alpha.

Все учётки, инструкции, заявки и шаг согласования в демо синтетические.

## Как это устроено

Go проверяет команду и владельца, ведёт черновик и сохраняет результат отправки. React и TypeScript отвечают за чат и страницу заявки, Zod проверяет ответы API. Имитация корпоративного сервиса записывает номер в отдельную таблицу. Подтверждение и запись результата выполняются в одной транзакции, поэтому повторы возвращают тот же номер.

Модель определяет намерение из текста и текущего состояния. Источник инструкции, права и разрешённые действия выбирает Go. Чат готовит черновик, отправка происходит после подтверждения на странице.

### MCP

Точка входа Streamable HTTP - `http://127.0.0.1:8080/mcp`. При обычном запуске задай `BUDDY_MCP_TOKEN`. В Compose есть тестовый токен `buddy-alpha-local-demo`. Передай клиенту заголовок `Authorization: Bearer <токен>` и проверь инструменты `ask`, `prepare_draft` и `request_status`. Тестовый токен связан с `demo-intern-1`. Адрес для ссылок задаётся через `BUDDY_PUBLIC_URL`.

### Qwen3-1.7B с LoRA

Опциональный режим использует адаптер Артемия из репозитория. Перед первым запуском скрипт скачивает около 4 ГБ официальных весов **Qwen/Qwen3-1.7B** с Hugging Face по фиксированной ревизии и сверяет SHA-256. Для загрузки не нужен ключ. При обрыве повтори команду скачивания.

Команды для PowerShell с Python 3.12 и CPU:

```powershell
python -m venv .venv
& ./.venv/Scripts/python.exe -m pip install torch==2.15.0 --index-url https://download.pytorch.org/whl/cpu
& ./.venv/Scripts/python.exe -m pip install -r requirements-ml.txt
& ./.venv/Scripts/python.exe tools/download_base_model.py
$env:BUDDY_ROUTER_KEY = 'local-demo-router'
$env:OMP_NUM_THREADS = '4'
$env:MKL_NUM_THREADS = '4'
& ./.venv/Scripts/python.exe -m ml.service
```

Во втором терминале запусти Go с этими переменными:

```powershell
$env:BUDDY_ROUTER_URL = 'http://127.0.0.1:8091/internal/v1/ml/route'
$env:BUDDY_ROUTER_KEY = 'local-demo-router'
go run .
```

Используй свой ключ вне локального демо. `/api/health` покажет `qwen-router`. Для CUDA нужна сборка Torch под установленный драйвер. Проверить модель отдельно можно командой `python scripts/check_adapter.py` с тем же ключом. [Отчёт прогона с реальным Qwen на CPU](reports/qwen-cpu-check.json).

## Тесты и отчёты

```bash
go vet ./...
go test -v "-coverprofile=backend.cover" ./...
pnpm --dir web test
python -m pip install Flask==3.1.3 jsonschema==4.25.1 coverage==7.13.4
python -m coverage run --source=ml -m unittest discover -s tests -v
python -m coverage json -o coverage.json
python scripts/coverage_report.py
```

Для Go-тестов на PostgreSQL укажи URL тестовой БД в `BUDDY_TEST_POSTGRES`. У её пользователя должно быть право создавать временные схемы. Браузерный сценарий запускается на чистой БД командой `python scripts/e2e.py --output e2e-evidence` после установки `playwright==1.65.2` и `python -m playwright install chromium`.

[GitHub Actions](https://github.com/Artur717661/AvitiHack/actions) прогоняет тесты и браузерный сценарий в Docker с PostgreSQL.

- [Контракт API](docs/draft-api.md)
- [Архитектура, метрики и план пилота](docs/product.md)
- [Карточка модели](docs/model-card.md)
- [Покрытие и команды воспроизведения](reports/coverage/summary.md)
- [Ревью перед сдачей](docs/review.md)
