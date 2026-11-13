# Docker

## Development

Требуются Docker Desktop с Linux containers, BuildKit и Compose 2.23+.
Команды выполняются из корня репозитория.

Если `.env` ещё нет: `Copy-Item .env-example .env`.
Заполните `POSTGRES_DB`, `POSTGRES_USER`, `POSTGRES_PASSWORD` и
`POSTGRES_PORT` (например, `5432`).
Для локального Go-запуска задайте `POSTGRES_HOST=127.0.0.1` и
`POSTGRES_SSLMODE=disable` (соединение без TLS в development).

Запуск приложения и PostgreSQL с обновлением Go-кода:

```powershell
docker compose --env-file .env -f docker/docker-compose.dev.yml up --build --watch
```

После лога `HTTP server listening on [::]:8080` (либо аналогичного адреса)
проверьте из другого терминала:

```powershell
curl.exe -i http://127.0.0.1:8080/health
docker compose --env-file .env -f docker/docker-compose.dev.yml ps
```

Ожидаются лог `PostgreSQL connected`, `200 OK` и PostgreSQL со статусом `healthy`.
Сохраните изменение в Go-файле: приложение должно пересобраться и перезапуститься.
Изменения `.env` применяются повторным `up --build --watch` после остановки.

## Что настроено

- `app.build` собирает образ из `Dockerfile.dev`; `.env` в образ не копируется.
- `env_file` передаёт `.env` контейнеру; `environment` переопределяет
  `HTTP_ADDR` на `:8080`, чтобы сервер принимал подключения извне контейнера.
- PostgreSQL также читает `.env` через `env_file`. Для подстановки
  `${POSTGRES_PORT}` в Compose нужен `--env-file .env`, как в команде запуска выше.
  При запуске из GoLand укажите корневой `.env` как файл переменных Compose.
- `develop.watch` пересобирает образ при изменении кода или файлов сборки.
  Ошибка компиляции не останавливает уже работающий контейнер.
- Бинарник собирается в образе и запускается напрямую, получая сигналы Docker.
  `RUN --mount=type=cache` сохраняет кэш компиляции между сборками.
- `postgres_data` хранит данные базы отдельно от контейнера.
- `depends_on` запускает приложение после готовности базы.
  Приложение проверяет соединение за 5 секунд; при ошибке HTTP не запускается.
- `stop_grace_period` даёт приложению 15 секунд на остановку.
  Приложение ограничивает собственное ожидание запросов 10 секундами.
- Оба опубликованных порта доступны только локально.

## База данных

С хоста: `127.0.0.1:POSTGRES_PORT`. Из `app`: `postgres:5432`.
В Compose параметры `POSTGRES_HOST` и `POSTGRES_PORT` для приложения
переопределяются на `postgres` и `5432`: опубликованный порт хоста здесь не используется.
`POSTGRES_USER` — superuser; это только development-окружение.

SQL-проверка (замените `chat`, если меняли пользователя или имя базы):

```powershell
docker compose --env-file .env -f docker/docker-compose.dev.yml exec postgres psql -h 127.0.0.1 -U chat -d chat -W
```

Введите пароль из `.env`, затем `SELECT current_database(), current_user;`.
Выход — `\q`. Healthcheck проверяет готовность базы, но не пароль.

## Остановка и данные

`Ctrl+C` останавливает режим разработки. Удалить контейнеры, сохранив данные:

```powershell
docker compose --env-file .env -f docker/docker-compose.dev.yml down
```

**`down -v` удаляет volumes вместе с данными.**
Параметры пользователя, пароля и базы применяются только при первом создании
пустого PostgreSQL volume. Изменение `.env` не меняет их в существующей базе.
Не обновляйте major-версию PostgreSQL поверх старого volume без миграции.

Проверка сохранности: создайте обычную тестовую таблицу со строкой через `psql`,
выполните `down`, запустите Compose снова и убедитесь, что строка сохранилась.
После проверки удалите тестовую таблицу.

## Тест подключения

Для integration-тестов нужна отдельная база с общими учётными данными.
Подготовка `POSTGRES_TEST_DB` и команды запуска описаны в
[`migrations/README.md`](../migrations/README.md#проверка-на-отдельной-базе).

## Swagger UI

```powershell
docker compose -f docker/docker-compose.swagger.yml up
```

Адрес: <http://localhost:8081/#/>. Остановка — `Ctrl+C`.

## Production

Пока не реализован. `Dockerfile.dev` предназначен только для разработки.
