# Roadmap Graph

Архив содержит исходники, собранный frontend и исполняемый файл для Linux x86-64 с glibc. SQLite включён в backend; отдельный сервер БД не нужен.

## Быстрый локальный запуск без Docker

Из каталога `roadmap-graph`:

```bash
read -rs -p 'Пароль экземпляра (не менее 12 символов): ' ROADMAP_PASSWORD
echo
export ROADMAP_PASSWORD
export ROADMAP_ADDR=127.0.0.1:8080
export ROADMAP_COOKIE_SECURE=false
export ROADMAP_STATIC="$PWD/frontend/dist"
export ROADMAP_DB="$PWD/data/roadmap.db"
export ROADMAP_DEMO=true
./backend/roadmap
```

Откройте http://localhost:8080 и введите заданный пароль. Для примера с PROJECT X, COMMON MODULE, PROJECT F и PROJECT Y нажмите **Load fictional demo** в пустом приложении. Пример вымышленный. Без `ROADMAP_DEMO=true` его загрузка запрещена. Рабочая база автоматически не заполняется.

Если Linux сообщает об отсутствии нужной версии glibc или используется другая архитектура, пересоберите backend по README или используйте Docker.

## Docker

```bash
cp .env.example .env
# Укажите собственный ROADMAP_PASSWORD в .env.
docker compose up --build -d
```

Compose привязывает порт к loopback. Данные сохраняются в именованном volume. Для доступа через Интернет нужен TLS reverse proxy и `ROADMAP_COOKIE_SECURE=true`.

## Что проверено

Актуальные результаты и незакрытые критерии приведены в [docs/VERIFICATION.md](docs/VERIFICATION.md). Не считать браузерный E2E и Docker-сборку успешными, пока они не выполнены в доступной среде.

## Структура

- `backend/` — Go API, SQLite, миграция и тесты.
- `frontend/` — Svelte 5, TypeScript, SVG/D3, тесты и `dist`.
- `scripts/smoke.py` — проверка реального процесса и сохранности базы после перезапуска.
- `docs/` — API, архитектура, результаты проверок и дальнейший roadmap.

Следующие инженерные приоритеты: браузерная проверка и Docker release gate; затем разруливание пересекающихся подписей плотного графа, защита от конфликтующих параллельных изменений и решение о развитии многопользовательской авторизации.
