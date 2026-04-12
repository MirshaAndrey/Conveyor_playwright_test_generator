# Conveyor

**Conveyor** — CLI-инструмент на Go для автоматической генерации, запуска и починки E2E-тестов (Playwright + TypeScript). Поддерживает локальные LLM через [Ollama](https://ollama.ai/), облачные модели Google Gemini API и Anthropic Claude API.

```
   ██████╗ ██████╗ ███╗   ██╗██╗   ██╗███████╗██╗   ██╗ ██████╗ ██████╗
  ██╔════╝██╔═══██╗████╗  ██║██║   ██║██╔════╝╚██╗ ██╔╝██╔═══██╗██╔══██╗
  ██║     ██║   ██║██╔██╗ ██║██║   ██║█████╗   ╚████╔╝ ██║   ██║██████╔╝
  ██║     ██║   ██║██║╚██╗██║╚██╗ ██╔╝██╔══╝    ╚██╔╝  ██║   ██║██╔══██╗
  ╚██████╗╚██████╔╝██║ ╚████║ ╚████╔╝ ███████╗   ██║   ╚██████╔╝██║  ██║
   ╚═════╝ ╚═════╝ ╚═╝  ╚═══╝  ╚═══╝  ╚══════╝   ╚═╝    ╚═════╝ ╚═╝  ╚═╝
        Playwright Test Generator  v1.3.0
```

---

## Содержание

1. [Возможности](#возможности)
2. [Установка](#установка)
3. [Быстрый старт](#быстрый-старт)
4. [Провайдеры LLM](#провайдеры-llm)
5. [Режимы работы](#режимы-работы)
6. [Агентный режим](#агентный-режим)
7. [Формат cases.json](#формат-casesjson)
8. [Конфигурация config.json](#конфигурация-configjson)
9. [Все флаги CLI](#все-флаги-cli)
10. [Сканер страниц](#сканер-страниц)
11. [Структура выходных файлов](#структура-выходных-файлов)
12. [Архитектура проекта](#архитектура-проекта)

---

## Возможности

- **Три провайдера LLM** — Ollama (локально, полная приватность), Google Gemini API, Anthropic Claude API
- **Четыре режима работы:** интерактивный, одиночный, пакетный (TXT/JSON), агентный
- **Автосканирование страниц** — headless Playwright сканирует `data-testid`, `aria-label`, кнопки, поля ввода и перехватывает API-запросы (XHR/fetch)
- **Generator → Reviewer** — второй LLM-проход исправляет код по детальным правилам
- **Валидация + Retry** — проверяет наличие `test.describe`, `expect()`, отсутствие `waitForTimeout`
- **Агентный режим** — полный цикл: scan → generate → review → run → fix loop → POM рефакторинг
- **Fix Loop** — при падении теста LLM читает ошибку Playwright и исправляет код автоматически
- **Перехват сети** — сканер автоматически собирает XHR/fetch-запросы страницы, фильтрует аналитику и дедуплицирует
- **Post-batch POM** — после генерации всех кейсов рефакторит тесты под Page Object Model
- **Умное сохранение** — файлы группируются по разделам: `TestCasesTS/<section>/`

---

## Установка

### Требования

| Компонент | Версия | Для чего |
|:---|:---|:---|
| [Go](https://go.dev/) | 1.22+ | Сборка Conveyor |
| [Node.js](https://nodejs.org/) | 18+ | Сканер страниц, запуск тестов |
| [Ollama](https://ollama.ai/) | любая | Локальные LLM (опционально) |

### Сборка

```bash
git clone https://github.com/mirshaandrey/conveyor.git
cd conveyor
go build -o conveyor .
```

### Установка зависимостей для сканера и запуска тестов

```bash
# В папке с бинарником conveyor:
npm install playwright
npx playwright install chromium
```

### Установка модели Ollama

```bash
ollama run qwen2.5-coder:7b
```

---

## Быстрый старт

### 1. Создайте `config.json`

```json
{
  "provider": "ollama",
  "ollama_model": "qwen2.5-coder:7b",
  "context": "React SPA, baseURL: https://app.example.com"
}
```

> ⚠️ Добавьте `config.json` в `.gitignore` — файл может содержать API ключ.

### 2. Создайте `cases.json`

```json
{
  "cases": [
    {
      "section": "auth",
      "name": "Вход в аккаунт",
      "url": "https://app.example.com/login",
      "task": "Авторизация с корректными учётными данными — успешный вход и редирект на главную",
      "prompt_hint": "Поля: data-testid=email-input, data-testid=password-input. Кнопка: data-testid=login-button"
    }
  ]
}
```

### 3. Запустите

```bash
# Пакетный режим — генерация тестов
./conveyor -cases-file cases.json

# Агентный режим — генерация + автозапуск + починка
./conveyor -agent -cases-file cases.json

# С Claude
./conveyor -cases-file cases.json -provider claude -key "sk-ant-..."

# С Gemini
./conveyor -cases-file cases.json -provider gemini -key "AIzaSy..."
```

---

## Провайдеры LLM

Conveyor поддерживает три провайдера. Выбор задаётся флагом `-provider` или полем `provider` в `config.json`.

### Ollama (локально)

Полная приватность — данные не покидают машину. Требуется запущенный сервер Ollama.

```bash
# Запуск
./conveyor -provider ollama -model qwen2.5-coder:7b

# Или с config.json
{
  "provider": "ollama",
  "ollama_url": "http://localhost:11434",
  "ollama_model": "qwen2.5-coder:7b"
}
```

При запуске без флага `-model` Conveyor покажет список установленных моделей для выбора.

### Google Gemini API

Облачный провайдер Google. Ключ передаётся через флаг `-key` или переменную `GEMINI_API_KEY`.

```bash
# Через флаг
./conveyor -provider gemini -key "AIzaSy..." -model gemini-2.5-flash

# Через переменную окружения
GEMINI_API_KEY="AIzaSy..." ./conveyor -provider gemini

# Через config.json
{
  "provider": "gemini",
  "gemini_api_key": "AIzaSy...",
  "gemini_model": "gemini-2.5-flash"
}
```

> ⚠️ Бесплатный тариф Gemini: между запросами автоматически добавляется пауза 3 сек.

### Anthropic Claude API

Облачный провайдер Anthropic. Ключ передаётся через флаг `-key` или переменную `ANTHROPIC_API_KEY`.

```bash
# Через флаг
./conveyor -provider claude -key "sk-ant-..."

# Через переменную окружения
ANTHROPIC_API_KEY="sk-ant-..." ./conveyor -provider claude

# Через config.json
{
  "provider": "claude",
  "claude_api_key": "sk-ant-...",
  "claude_model": "claude-sonnet-4-6"
}
```

**Доступные модели Claude:**

| Модель | Описание |
|:---|:---|
| `claude-sonnet-4-6` | Рекомендуется — баланс качества и скорости (по умолчанию) |
| `claude-opus-4-6` | Максимальное качество генерации |
| `claude-haiku-4-5-20251001` | Самый быстрый, подходит для массовой генерации |

При запуске без флага `-model` Conveyor покажет список моделей для выбора.

### Приоритет настроек

```
CLI флаги (-key, -model, -provider)
       ↓
Переменные окружения (GEMINI_API_KEY, ANTHROPIC_API_KEY)
       ↓
config.json (gemini_api_key, claude_api_key, ...)
       ↓
Встроенные дефолты
```

---

## Режимы работы

### Интерактивный

```bash
./conveyor
```

Программа спросит провайдера и модель, затем ждёт ввода задачи в консоли. Ответ стримится в реальном времени.

### Одиночный

```bash
./conveyor -task "Авторизация с неверным паролем: ожидается сообщение об ошибке"
```

### Пакетный — JSON (рекомендуется)

```bash
./conveyor -cases-file cases.json
```

Файлы сохраняются в `TestCasesTS/<section>/`.

### Пакетный — TXT

```bash
./conveyor -tasks-file cases.txt
```

Простой список задач, по одной строке. Файлы сохраняются в `TestCasesTS/`.

### С ревью

```bash
./conveyor -cases-file cases.json -review
```

После генерации каждого теста запускается второй LLM-проход — Reviewer проверяет код по детальным правилам и исправляет его.

### С автосканированием страницы

```bash
./conveyor -cases-file cases.json -scan-url https://app.example.com/login
```

Перед генерацией Conveyor сканирует страницу и собирает все `data-testid`, кнопки, поля и API-запросы. Результат автоматически подставляется в промпт. Если в `cases.json` у кейса задано поле `url` — сканирование происходит для каждого кейса отдельно.

---

## Агентный режим

Агентный режим — полный автоматический pipeline. Для каждого тест-кейса выполняется следующая цепочка:

```
cases.json
    │
    ▼
[ ⊕ SCAN ]      Если задан url — сканируем страницу (DOM + сетевые запросы)
    │
    ▼
[ ~ GENERATE ]  LLM генерирует тест (с retry при ошибке валидации)
    │
    ▼
[ » REVIEW ]    (если -review) Второй LLM-проход исправляет код
    │
    ▼
[ ► TEST ]      npx playwright test запускает тест
    │
    ├── PASS ──► [ + DONE ]
    │
    └── FAIL ──► [ ↻ FIX 1/3 ] LLM читает ошибку и исправляет код
                      │
                      ▼
                 [ ► TEST ] повторный запуск
                      │
                      ├── PASS ──► [ + DONE ]
                      └── FAIL ──► [ ↻ FIX 2/3 ] ... до -max-fix попыток
                                        │
                                        └── FAIL ──► [ x FAILED ]

После всех кейсов (если -pom):
[ POM ] Все тесты уходят в LLM → рефакторинг под Page Object Model
        Результат сохраняется в TestCasesTS/pom/
```

### Команды агентного режима

```bash
# Базовый — генерация + запуск + починка
./conveyor -agent -cases-file cases.json

# С ревью перед запуском
./conveyor -agent -cases-file cases.json -review

# Увеличить лимит попыток починки
./conveyor -agent -cases-file cases.json -max-fix 5

# Только генерация без запуска тестов
./conveyor -agent -cases-file cases.json -no-run

# Полный pipeline с POM рефакторингом в конце
./conveyor -agent -cases-file cases.json -review -pom

# С Claude
./conveyor -agent -cases-file cases.json -provider claude -key "sk-ant-..."

# С Gemini
./conveyor -agent -cases-file cases.json -provider gemini -key "AIzaSy..."
```

### Индикаторы трекера в агентном режиме

| Иконка | Цвет | Статус |
|:---:|:---:|:---|
| `[ ]` | серый | Ожидает |
| `[⊕]` | фиолетовый | Сканирование страницы |
| `[~]` | жёлтый | Генерация теста |
| `[»]` | голубой | Ревью кода |
| `[►]` | синий | Запуск теста |
| `[↻]` | жёлтый | Исправление ошибки (1/3) |
| `[+]` | зелёный | Готово |
| `[x]` | красный | Ошибка |

---

## Формат cases.json

```json
{
  "cases": [
    {
      "section": "auth",
      "name": "Вход в аккаунт",
      "url": "https://app.example.com/login",
      "task": "Авторизация с корректными данными — вход и редирект на /dashboard",
      "prompt_hint": "Поля: data-testid=email-input, data-testid=password-input. Кнопка: data-testid=login-button"
    },
    {
      "section": "cart",
      "name": "Добавление товара",
      "url": "https://app.example.com/product/123",
      "task": "Нажать Add to Cart — товар появляется в корзине",
      "prompt_hint": "Кнопка: data-testid=add-to-cart. Счётчик: data-testid=cart-count"
    }
  ]
}
```

### Поля

| Поле | Обязательное | Описание |
|:---|:---:|:---|
| `section` | ✓ | Раздел — создаёт подпапку `TestCasesTS/<section>/` |
| `name` | ✓ | Короткое имя — в трекере и в имени файла |
| `task` | ✓ | Полное описание — уходит в промпт LLM |
| `url` | — | URL для автосканирования. Сканер собирает DOM-элементы и API-запросы |
| `prompt_hint` | — | Ручные подсказки — дополняют результат сканирования |

**Приоритет контекста:** `url` сканирование (DOM + API) + `prompt_hint` + `context` из `config.json`

---

## Конфигурация config.json

```json
{
  "provider": "ollama",
  "gemini_api_key": "",
  "gemini_model": "gemini-2.5-flash",
  "claude_api_key": "",
  "claude_model": "claude-sonnet-4-6",
  "ollama_url": "http://localhost:11434",
  "ollama_model": "qwen2.5-coder:7b",
  "context": "React SPA, baseURL: https://app.example.com, JWT авторизация",
  "system_prompt": "",
  "review_mode": false,
  "max_retries": 2
}
```

### Поля

| Поле | По умолчанию | Описание |
|:---|:---|:---|
| `provider` | `ollama` | Провайдер: `ollama`, `gemini` или `claude` |
| `gemini_api_key` | `""` | Ключ Google Gemini API |
| `gemini_model` | `gemini-2.5-flash` | Модель Gemini |
| `claude_api_key` | `""` | Ключ Anthropic Claude API |
| `claude_model` | `claude-sonnet-4-6` | Модель Claude |
| `ollama_url` | `http://localhost:11434` | Адрес сервера Ollama |
| `ollama_model` | `mistral-small3.2:latest` | Модель Ollama |
| `context` | `""` | Описание приложения — подставляется в `{{context}}` системного промпта |
| `system_prompt` | встроенный | Полная замена системного промпта. Поддерживает `{{context}}` |
| `review_mode` | `false` | Включить Reviewer для всех запусков |
| `max_retries` | `2` | Повторов при ошибке валидации LLM-ответа |

**Приоритет настроек:** CLI флаги > переменные окружения (`GEMINI_API_KEY`, `ANTHROPIC_API_KEY`) > `config.json` > встроенные дефолты.

---

## Все флаги CLI

### Основные

| Флаг | По умолчанию | Описание |
|:---|:---|:---|
| `-provider` | `""` | Провайдер LLM: `ollama`, `gemini` или `claude` |
| `-key` | `""` | API ключ (Gemini: `GEMINI_API_KEY`; Claude: `ANTHROPIC_API_KEY`) |
| `-model` | `""` | Имя модели (зависит от провайдера) |
| `-config` | `config.json` | Путь к файлу конфигурации |
| `-url` | `http://localhost:11434` | Адрес сервера Ollama |

### Входные данные

| Флаг | По умолчанию | Описание |
|:---|:---|:---|
| `-task` | `""` | Одиночный тест-кейс (интерактивный/одиночный режим) |
| `-tasks-file` | `""` | Файл со списком задач построчно `.txt` |
| `-cases-file` | `""` | Файл тест-кейсов в JSON формате `.json` |

### Генерация

| Флаг | По умолчанию | Описание |
|:---|:---|:---|
| `-review` | `false` | Включить второй проход Reviewer |
| `-scan-url` | `""` | URL для сканирования селекторов и API-запросов (глобально) |

### Сохранение

| Флаг | По умолчанию | Описание |
|:---|:---|:---|
| `-out` | `""` | Путь для сохранения (только одиночный режим) |
| `-no-save` | `false` | Не сохранять файлы автоматически |

### Агентный режим

| Флаг | По умолчанию | Описание |
|:---|:---|:---|
| `-agent` | `false` | Включить агентный режим (требует `-cases-file`) |
| `-max-fix` | `3` | Максимум попыток автоисправления упавшего теста |
| `-no-run` | `false` | Агент: только генерация, без запуска тестов |
| `-pom` | `false` | Агент: post-batch POM рефакторинг после всех кейсов |

---

## Сканер страниц

Conveyor автоматически сканирует страницу через headless Chromium и собирает два типа данных:

### DOM-элементы

- Все `data-testid` атрибуты с тегом, типом и текстом
- `aria-label` атрибуты
- Кнопки (`button`, `[role=button]`, `input[type=submit]`)
- Поля ввода с `label`, `placeholder`, `name`
- Ссылки (до 30)
- Заголовки `h1`/`h2`/`h3`

### API-запросы (XHR/fetch)

Сканер перехватывает все XHR и fetch-запросы которые страница делает при загрузке:

- **Метод** — GET, POST, PUT, DELETE и т.д.
- **Путь** — эндпоинт API
- **HTTP-статус** — код ответа (200, 401, 404...)
- **Тело запроса** — помечается для POST/PUT/PATCH

Результат автоматически подставляется в промпт и подсказывает LLM использовать `page.waitForResponse()` вместо ненадёжных `waitForTimeout`.

**Что фильтруется:** запросы к доменам аналитики (Google Analytics, Hotjar, Sentry, Facebook и т.д.) автоматически исключаются. Одинаковые запросы дедуплицируются.

**Пример вывода в промпт:**

```
--- API запросы (XHR/fetch) ---
  POST   200  /api/auth/login  [body]
  GET    200  /api/users/me
  GET    200  /api/products?category=shoes
  DELETE 204  /api/cart/items/42

  Hint: используй page.waitForResponse() или page.waitForRequest()
        для ожидания завершения API-вызовов в тестах.
```

### Предварительные требования

```bash
npm install playwright
npx playwright install chromium
```

### Использование

**Глобально** — один URL для всего запуска:
```bash
./conveyor -cases-file cases.json -scan-url https://app.example.com/login
```

**Индивидуально** — URL на каждый кейс в `cases.json`:
```json
{ "url": "https://app.example.com/login", ... }
```

**Комбинированно** — ручной `prompt_hint` дополняет результат сканирования:
```json
{
  "url": "https://app.example.com/login",
  "prompt_hint": "После успешного входа редирект на /dashboard"
}
```

### Как это работает

1. JS-скрипт пишется во временный `.js` файл рядом с `node_modules`
2. Запускается через `node` с `NODE_PATH` указывающим на `node_modules`
3. **Подписка на события `request` и `response` перед навигацией** — перехватывает все XHR/fetch
4. Playwright открывает страницу, ждёт `networkidle`
5. Извлекает DOM-элементы через `page.evaluate()`
6. Мержит DOM-данные с перехваченными сетевыми запросами
7. Возвращает единый JSON → форматируется в текстовый контекст → подставляется в промпт
8. Временный файл удаляется

**Статистика в консоли:**

```
[SCAN OK] "My App — Login"
          testid:12  aria:5  buttons:3  inputs:4  links:8  api:6
```

---

## Структура выходных файлов

### Пакетный режим с `cases.json`

```
TestCasesTS/
  auth/
    2026-04-12_10-00-00_vkhod_v_akkaunt.ts
    2026-04-12_10-00-15_registratsiya.ts
  cart/
    2026-04-12_10-00-30_dobavlenie_tovara.ts
  checkout/
    2026-04-12_10-00-45_oformlenie_zakaza.ts
```

### Пакетный режим с `cases.txt`

```
TestCasesTS/
  2026-04-12_10-00-00_vkhod_v_akkaunt.ts
  2026-04-12_10-00-15_poisk_tovara.ts
```

### Агентный режим с `-pom`

```
TestCasesTS/
  auth/
    2026-04-12_10-00-00_vkhod_v_akkaunt.ts   ← оригинальный тест
  pom/
    pages/
      LoginPage.ts                            ← Page Object классы
      CartPage.ts
    specs/
      login.spec.ts                           ← рефакторированные spec файлы
      cart.spec.ts
```

---

## Архитектура проекта

```
conveyor/
├── main.go       — CLI флаги, выбор провайдера, роутинг между режимами
├── agent.go      — Агентный loop: scan→generate→review→run→fix→pom
├── batch.go      — Пакетный режим, BatchConfig, generateOne (retry + review)
├── runner.go     — Запуск npx playwright test, парсинг ошибок
├── scanner.go    — Headless сканер (DOM + перехват сети), встроенный JS-скрипт
├── prompt.go     — Все системные промпты, buildCasePrompt, validateContent
├── ollama.go     — API Ollama: sendRequest, pickModel, checkOllama
├── gemini.go     — API Gemini: sendGeminiRequest, SSE-стриминг
├── claude.go     — API Claude: sendClaudeRequest, SSE-стриминг
├── config.go     — Config struct, loadConfig
├── files.go      — TestCase struct, loadCasesJSON, slug-генерация, сохранение
├── tracker.go    — Терминальный UI: прогресс-бар, статусы, Summary
├── cases.json    — Пример тест-кейсов
├── config.json   — Конфигурация (не коммитить!)
├── package.json  — Node.js зависимости (playwright)
└── playwright.config.ts — Конфиг для запуска сгенерированных тестов
```

### Поток данных

```
cases.json ──► loadCasesJSON ──► TestCase[]
                                    │
                         ┌──────────┤
                         │          │
                    [agent mode] [batch mode]
                         │          │
                    runAgent    runBatchCases
                         │          │
                    processOneCase  generateOne
                         │
              ┌──────────┼──────────┐
              │          │          │
         runScanner  generateOne  runPlaywrightTest
              │          │          │
    ┌─────────┤     cleanCode   RunResult
    │         │          │          │
  DOM scan  Network   validate    parseErrors
    │       capture      │          │
    └────┬────┘     saveResult   fixLoop ──► doFix ──► LLM
         │                                      │
    ScanResult                            ┌─────┴──────────┐
         │                                │                │
    formatScan ──► prompt           Ollama│Gemini│Claude
```

---

## Примеры использования

### Минимальный запуск

```bash
./conveyor -task "Проверить форму входа на /login"
```

### Пакетная генерация с Claude

```bash
./conveyor -cases-file cases.json -provider claude -key "sk-ant-..."
```

### Пакетная генерация с Gemini

```bash
./conveyor -cases-file cases.json -provider gemini -key "AIzaSy..."
```

### Агент с полным pipeline

```bash
./conveyor -agent -cases-file cases.json -review -pom -max-fix 5
```

### Агент с Claude и полным pipeline

```bash
./conveyor -agent -cases-file cases.json -provider claude -key "sk-ant-..." -review -pom
```

### Локально с предварительным сканированием

```bash
./conveyor -cases-file cases.json -scan-url https://localhost:3000/login
```

### Только генерация без сохранения (preview)

```bash
./conveyor -task "Тест авторизации" -no-save
```

### Кросс-компиляция

```bash
# Windows
GOOS=windows GOARCH=amd64 go build -o conveyor.exe .

# Linux
GOOS=linux GOARCH=amd64 go build -o conveyor-linux .

# macOS Apple Silicon
GOOS=darwin GOARCH=arm64 go build -o conveyor-mac-arm .
```

---

## Contributing

Нашли баг или есть идея — создайте Issue или Pull Request на [GitHub](https://github.com/mirshaandrey/conveyor).
