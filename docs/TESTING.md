# Тестирование

Команды выполняются из корня репозитория.

## Unit- и компонентные тесты

Находятся рядом с проверяемым кодом и не требуют PostgreSQL:

```powershell
go test ./...
```

## Подготовка integration-тестов

Проверки реальной PostgreSQL находятся в `tests/integration` и включаются
тегом `integration`. Их помощники размещены в `*_test.go` того же каталога.

Если корневого `.env` ещё нет:

```powershell
Copy-Item .env-example .env
```

Заполните параметры подключения. Пример для локальной разработки:

```dotenv
POSTGRES_HOST=127.0.0.1
POSTGRES_PORT=5432
POSTGRES_DB=chat
POSTGRES_USER=chat
POSTGRES_PASSWORD=YOUR_PASSWORD
POSTGRES_SSLMODE=disable
POSTGRES_TEST_DB=chat_test
```

Используйте фактические учётные данные development PostgreSQL вместо
`YOUR_PASSWORD`. В существующем `.env` достаточно добавить `POSTGRES_TEST_DB`:
адрес, порт, пользователь, пароль и SSL-режим общие с приложением.

Имя тестовой базы обязательно, должно оканчиваться на `_test` и отличаться
от `POSTGRES_DB`. При неверной настройке или недоступной БД тесты завершаются
ошибкой, а не пропускаются. Автоматического перехода на development-базу нет.

Тесты читают корневой `.env` независимо от рабочей директории, включая GoLand.
Переменные окружения имеют приоритет, даже если они пустые.

Запустите PostgreSQL:

```powershell
docker compose --env-file .env -f docker/docker-compose.dev.yml up -d --wait postgres
```

Создайте тестовую базу один раз. Если она уже существует, команду пропустите:

```powershell
docker compose --env-file .env -f docker/docker-compose.dev.yml exec postgres createdb -U chat chat_test
```

Если меняли пользователя или имя тестовой базы, замените аргументы команды.
Другие инструкции по Docker находятся в [`docker/README.md`](../docker/README.md).

## Запуск integration-тестов

```powershell
go test -tags=integration -v -count=1 ./tests/integration
```

Тест подключения выполняет `SELECT 1` и проверяет закрытие пула. Миграционные
сценарии создают случайные схемы внутри тестовой БД и удаляют только их
после завершения. Прикладных SQL-миграций пока нет; SQL этих проверок находится
в тестовых фикстурах.

## Форматирование и статические проверки

```powershell
gofmt -w .
go vet ./...
go vet -tags=integration ./tests/integration
```

CI запускает оба набора тестов с `-race`; workflow описан в
[`CONTRIBUTING.md`](CONTRIBUTING.md#ci). Для локального запуска race detector
на Windows дополнительно нужен поддерживаемый C-компилятор.
