# kimi-lite — Полный аудит кода

**Дата:** 2026-06-09  
**Аудиторы:** 7 параллельных агентов по направлениям  
**Объём:** ~50 файлов, ~5000 строк кода, ~200 тестов

---

## Итоговые оценки по направлениям

| Направление | Оценка | Ключевая проблема |
|-------------|--------|-------------------|
| Качество кода (Go-идиомы) | **6/10** | Игнорирование ошибок, глобальное состояние, data race |
| Архитектура и дизайн | **4/10** | Approval-система и MCP — фасад, не работают |
| Тесты и покрытие | **6/10** | 73.2% покрытие, но 0% у cmd/ и config/ |
| Безопасность и ошибки | **4/10** | Нет sandbox, произвольный shell/file доступ |
| Производительность | **5/10** | O(n²) рендеринг, утечка горутин, infinite recursion |
| TUI/UX | **4/10** | Нет фокус-менеджмента, approval-диалог отсутствует |
| CI/CD и сборка | **5/10** | CGO+sqlite = сломанный релиз |
| **Общая оценка** | **4.9/10** | **Не production-ready без исправления P0** |

---

## P0 — Критические (не production-ready)

### 1. Approval-система не работает
**Файлы:** `internal/core/turn.go:236-269`, `internal/tui/model.go:534-543`, `internal/tui/styles/styles.go:208-213`

`TurnManager.executeToolCalls()` при manual approval ставит статус `TurnWaitingApproval`, сохраняет turn — и **сразу продолжает выполнение**, добавляя `ToolResult{Error: "tool call requires manual approval"}`. Второй LLM-вызов идёт с этой ошибкой в контексте. Пользователь видит статус "approval" в статус-баре, но диалога нет. Константы `ApprovalAlways` и `ApprovalDiff` — мёртвый код.

**Исправление:** Переделать `TurnManager` на pause-resume цикл. При manual approval возвращать "waiting approval" из `RunTurn`, добавить `ResumeWithApproval()`. В TUI рендерить модальное окно с diff.

---

### 2. MCP-интеграция чисто косметическая
**Файлы:** `internal/app/app.go:163-168`, `internal/core/turn.go:92`

`App.Run()` подключает MCP, считает инструменты, показывает `tools: N+M` в статус-баре. Но `TurnManager` вызывает **только** `tm.tools.Definitions()` (built-in) и `tm.tools.Execute()` (built-in executor). MCP-инструменты никогда не попадают в LLM-запрос и никогда не исполняются.

**Исправление:** Создать `CompositeToolExecutor` — агрегатор `[]api.ToolExecutor`. Передать в `TurnManager`. Реализовать `MCPToolExecutor` обёртку над `api.MCPClient`.

---

### 3. Нет sandbox для файловых инструментов
**Файлы:** `internal/core/tools.go:204-302`

`read_file`, `write_file`, `str_replace_file`, `glob`, `grep` принимают произвольные пути. `write_file` создаёт директории и перезаписывает любой файл. `shell` выполняет `sh -c` с произвольной командой. В yolo-режиме LLM может читать `~/.ssh/id_rsa`, перезаписывать системные файлы.

**Исправление:** Добавить `Sandbox` с `AllowedPaths []string` (по умолчанию — рабочая директория сессии). Проверять `filepath.Clean` + `strings.HasPrefix` для всех file-инструментов. Для shell — `cmd.Dir = sessionPath`, denylist опасных команд.

---

### 4. Релизный pipeline сломан (CGO + sqlite3)
**Файлы:** `.goreleaser.yml:15`, `Makefile:28-32`

`.goreleaser.yml` устанавливает `CGO_ENABLED=0`, но проект зависит от `mattn/go-sqlite3` (CGO-binding к C-библиотеке). Бинарник скомпилируется, но при первом обращении к БД упадёт с ошибкой. README заявляет "single static binary, Alpine Linux, musl" — это ложь.

**Исправление:** Мигрировать на `modernc.org/sqlite` (pure-Go SQLite). После этого `CGO_ENABLED=0` станет валидным для всех платформ.

---

### 5. TUI — нет фокус-менеджмента, все компоненты получают все клавиши
**Файлы:** `internal/tui/model.go:262-283`

Каждое `tea.KeyMsg` передаётся **всем** дочерним компонентам. `↑` в input двигает историю **и** курсор в sidebar, **и** скроллит viewport. `Enter` отправляет сообщение **и** тогглит tool call. UI борется с пользователем.

**Исправление:** Ввести `focusedComponent` enum. `tab` / `shift+tab` переключают фокус. `tea.KeyMsg` роутить только в focused-компонент.

---

### 6. Viewport — клавиши обрабатываются дважды
**Файлы:** `internal/tui/viewport/viewport.go:42-81`

Кастомный viewport обрабатывает `pgup/pgdown/home/end/↑/↓` самостоятельно, а затем передаёт тот же `tea.KeyMsg` во внутренний `bubbles/viewport`, который обрабатывает их **ещё раз**.

**Исправление:** Фильтровать эти клавиши от передачи во внутренний viewport.

---

### 7. Sidebar — бесконечная рекурсия без ограничения глубины
**Файлы:** `internal/tui/sidebar/sidebar.go:318`

```go
if info.IsDir() && (maxDepth == 0 || maxDepth > 0) // всегда true
```

Условие тавтологическое. `buildTree` обходит **всё** дерево директорий. На большом репозитории с `node_modules` TUI зависнет при старте.

**Исправление:** `if info.IsDir() && maxDepth > 0`. Передавать `maxDepth: 2` или 3 из `refresh()`.

---

## P1 — Высокий приоритет

### 8. context.Background() вместо propagated context
**Файлы:** `internal/tui/model.go:393`, `internal/app/app.go:79`

`handleSend` создаёт `context.WithCancel(context.Background())` вместо использования app-контекста. SIGINT не отменяет LLM-запросы — только Esc. MCP-подключение при старте тоже через `context.Background()` без таймаута.

**Исправление:** Хранить root `context.Context` в `tui.Model`, передавать из `main`. Для MCP — `context.WithTimeout(..., 5s)`.

---

### 9. Множество игнорируемых ошибок
**Файлы:** `internal/core/turn.go:94,122,134,147,173,277`, `internal/app/app.go:173,181`

`_ = tm.saveTurn(...)`, `_ = a.store.AppendMessage(...)` — ошибки persistence молча проглатываются. Пользователь думает, что сессия сохранена, а она не сохранена.

**Исправление:** Убрать все `_ =` для persistence-операций. Логировать через `slog`, либо возвращать ошибки в UI.

---

### 10. Goroutine leak в LLM streaming
**Файлы:** `internal/llm/client.go:109`

Канал `make(chan api.StreamChunk)` небуферизованный. Если consumer перестаёт читать (например, пользователь нажал Esc), goroutine навсегда блокируется на send. `defer cancel()` внутри goroutine никогда не выполнится.

**Исправление:** `make(chan api.StreamChunk, 64)`. Убедиться, что consumer всегда drain'ит или отменяет.

---

### 11. O(n²) рендеринг viewport
**Файлы:** `internal/tui/model.go:564-571`

`refreshViewport()` склеивает **все** сообщения в строку на каждом кадре. `msg.View()` для assistant-сообщений вызывает `glamour.Render()` — полный Markdown-пайплайн. При 100 сообщениях каждый чанк стрима вызывает 100 ререндеров.

**Исправление:** Кешировать отрендеренные сообщения. Инвалидировать только последнее assistant-сообщение во время стрима. Использовать append-only builder.

---

### 12. Glamour re-render на каждый чанк стрима
**Файлы:** `internal/tui/messages/messages.go:185-196`

`AppendContent` инвалидирует кеш и вызывает `glamour.Render()` на каждый `View()`. 500 токенов = ~500 полных рендеров Markdown + HTML sanitizer + Chroma.

**Исправление:** Debounce: рендерить Glamour не чаще раза в 200ms или только при завершении стрима. Во время стрима показывать raw text.

---

### 13. `fetch_url` — SSRF, отсутствие лимитов
**Файлы:** `internal/core/tools.go:304-323`

Принимает любой URL без валидации схемы. Разрешены `file://`. Нет фильтрации private IP. Нет лимита размера ответа. Используется `http.DefaultClient` без таймаута.

**Исправление:** Валидировать схему (`http`/`https` only). Блокировать `localhost`, `169.254.x.x`, `10.x.x.x` и т.д. `io.LimitReader` на 10MB. Кастомный `http.Client` с таймаутом.

---

### 14. 0% покрытие cmd/ и config/
**Файлы:** `cmd/kimi-lite/main.go`, `internal/config/`

Критически важный пользовательский код полностью не покрыт тестами. Флаги, resolution сессий, загрузка конфига — всё это может сломаться без заметных ошибок компиляции.

**Исправление:** Рефакторить `run()` в testable-функцию с инжектируемыми зависимостями. Тесты для `Loader.Load()` с временными файлами.

---

## P2 — Средний приоритет

### 15. `map[string]interface{}` для JSON Schema
**Файлы:** `pkg/api/types.go:49`, `internal/core/tools.go:45-157`

`ToolDefinition.Parameters` — `map[string]interface{}`. Все JSON Schema определения — огромные вложенные `map[string]interface{}`. Опечатки в ключах — runtime-ошибки.

**Исправление:** Заменить на `json.RawMessage` или типизированные структуры.

---

### 16. `api.Store` нарушает ISP
**Файлы:** `pkg/api/types.go:114-134`

10 методов в одном интерфейсе. `ContextCompressor` нужен только `GetMessages`/`ClearMessages`/`AppendMessage`.

**Исправление:** Разбить на `SessionStore`, `MessageStore`, `TurnStore`. `Store` = embed всех трёх.

---

### 17. `api.Config` содержит `mapstructure`-теги
**Файлы:** `pkg/api/types.go:191-243`

Публичный пакет `api` заявлен как "для внешних инструментов", но содержит теги Viper — leak implementation details.

**Исправление:** Перенести `mapstructure`-теги во внутренний `internal/config.RawConfig`, маппить в чистый `api.Config`.

---

### 18. Data race в tui.Model
**Файлы:** `internal/tui/model.go:362-366`, `model.go:392-394`

`handleKeyMsg` читает `m.streamCancel`/`m.streamCh` без `m.mu`, в то время как `handleSend` пишет под `mu`. Race detector может это не поймать в текущих тестах.

**Исправление:** Брать `m.mu.Lock()` в `handleKeyMsg` перед чтением streaming-полей.

---

### 19. `http.DefaultClient` в `fetch_url`
**Файлы:** `internal/core/tools.go:313`

Нет инжекции `*http.Client`. Нельзя замокать в тестах, нельзя настроить таймауты/прокси.

**Исправление:** Добавить `httpClient *http.Client` в `BuiltInToolExecutor`.

---

### 20. `time.Sleep` в тестах store
**Файлы:** `internal/store/sqlite_test.go:88,111,151,522,524`

5 тестов используют `time.Sleep(10ms)` для форсирования порядка по `updated_at`. На медленном CI может быть недостаточно.

**Исправление:** Инжектировать clock-интерфейс или использовать hardcoded timestamps.

---

### 21. `generateID()` продублирован
**Файлы:** `internal/core/id.go:12`, `internal/store/sqlite.go:42`

Одинаковая функция в двух пакетах. Риск расхождения.

**Исправление:** Вынести в `pkg/id` или `internal/idgen`.

---

### 22. Viper — лишний вес
**Файлы:** `go.mod`

Viper тянет HCL, YAML, INI, mapstructure, afero, fsnotify. Для одного TOML-файла — overkill. Добавляет 2-3MB к бинарнику.

**Исправление:** Заменить на `github.com/BurntSushi/toml` или `github.com/pelletier/go-toml/v2`.

---

### 23. `.golangci.yml` — недостаточно линтеров
**Файлы:** `.golangci.yml:6-16`

Нет `gosec` (shell execution), `rowserrcheck` (DB), `revive` (idioms), `wrapcheck` (error wrapping).

**Исправление:** Добавить `gosec`, `rowserrcheck`, `revive`, `wrapcheck`, `errchkjson`.

---

### 24. CI не генерирует coverage
**Файлы:** `.github/workflows/ci.yml:46-56`

`go test -race ./...` без `-coverprofile`, но затем загружается `./coverage.out` в Codecov. Файла нет.

**Исправление:** `go test -race -coverprofile=coverage.out ./...` или `make test`.

---

## Что сделано хорошо

1. **Чистая архитектура зависимостей.** Все пакеты зависят от `pkg/api` внутрь. Нет циклических зависимостей. DI-контейнер в `internal/app` аккуратно связывает всё в одном месте.

2. **Минимальные интерфейсы.** `LLMClient`, `Store`, `ToolExecutor`, `ApprovalGate`, `GitProvider`, `MCPClient` — каждый отвечает за одно. Легко мокировать.

3. **Context propagation.** Почти все долгие операции (HTTP, DB, exec) принимают и уважают `context.Context`. `TurnManager.consumeStream` проверяет `ctx.Err()` на границах.

4. **Качественные тесты у core/llm/store.** Table-driven, `t.Parallel()`, тесты на отмену контекста, race-условия, retry-логику. `internal/llm/client_test.go` — образец тестирования HTTP-клиента.

5. **Компонентная декомпозиция TUI.** Input, viewport, sidebar, messages — каждый — отдельный Bubble Tea model. Следует Elm-архитектуре.

6. **Git provider с `cmdRunner` интерфейсом.** Полностью мокабельно без реальных shell-вызовов. Хороший паттерн для портфолио.

7. **SQLite с parameterized queries.** Ноль dynamic SQL concatenation. SQL injection невозможен.

---

## Приоритизированный план исправлений

### Спринт 1 (критический, 2-3 дня)
1. Миграция на `modernc.org/sqlite` + `CGO_ENABLED=0`
2. Фикс sidebar infinite recursion (`maxDepth > 0`)
3. Фокус-менеджмент в TUI
4. Fix viewport double-keypress
5. Sandbox для файловых инструментов

### Спринт 2 (функциональность, 3-4 дня)
6. Реальный approval-диалог (pause-resume TurnManager)
7. CompositeToolExecutor + рабочий MCP
8. Hardening `fetch_url` (SSRF, лимиты)
9. context propagation через TUI
10. Убрать все `_ =` для persistence

### Спринт 3 (производительность, 2 дня)
11. Debounce Glamour rendering
12. Append-only viewport builder
13. Буферизация stream channel (64)
14. Lazy loading для sidebar

### Спринт 4 (тесты и CI, 2 дня)
15. Тесты для `cmd/kimi-lite` и `internal/config`
16. Fix CI coverage generation
17. Добавить linters в `.golangci.yml`
18. Убрать `time.Sleep` из store-тестов
19. Дедупликация `generateID()`
