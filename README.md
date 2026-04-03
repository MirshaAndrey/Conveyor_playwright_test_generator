# Conveyor

**Conveyor** — легковесный CLI-инструмент на Go для автоматической генерации E2E-тестов (Playwright + TypeScript) с использованием локальных LLM через [Ollama](https://ollama.ai/) или облачных моделей через **Google Gemini API**.

```text
   ██████╗ ██████╗ ███╗   ██╗██╗   ██╗███████╗██╗   ██╗ ██████╗ ██████╗ 
  ██╔════╝██╔═══██╗████╗  ██║██║   ██║██╔════╝╚██╗ ██╔╝██╔═══██╗██╔══██╗
  ██║     ██║   ██║██╔██╗ ██║██║   ██║█████╗   ╚████╔╝ ██║   ██║██████╔╝
  ██║     ██║   ██║██║╚██╗██║╚██╗ ██╔╝██╔══╝    ╚██╔╝  ██║   ██║██╔══██╗
  ╚██████╗╚██████╔╝██║ ╚████║ ╚████╔╝ ███████╗   ██║   ╚██████╔╝██║  ██║
   ╚═════╝ ╚═════╝ ╚═╝  ╚═══╝  ╚═══╝  ╚══════╝   ╚═╝    ╚═════╝ ╚═╝  ╚═╝
        Playwright Test Generator  v1.1.0
```

## Особенности

- **Мультипровайдерность** — локальные модели через Ollama (полная приватность) и облачные через Google Gemini API.
- **Три режима работы**:
  - *Интерактивный* — ввод тест-кейса вручную со стримингом ответа в консоль.
  - *Одиночный* — передача задачи через флаг `-task`.
  - *Пакетный (Batch)* — обработка списка кейсов из файла с in-place прогресс-баром.
- **Два формата входных данных**:
  - `.txt` — простой список задач, по одной на строку (`-tasks-file`).
  - `.json` — структурированные кейсы с разделами и подсказками по селекторам (`-cases-file`).
- **Generator → Reviewer** — опциональный второй LLM-проход: модель проверяет и исправляет сгенерированный тест по детальным правилам (флаг `-review`).
- **Валидация + Retry** — автоматическая проверка ответа на наличие `test.describe`, `expect()`, отсутствие `waitForTimeout`. При провале — до `max_retries` повторных попыток.
- **Очистка markdown** — автоматически убирает обёртки ` ```typescript ``` ` из ответа модели перед сохранением.
- **Умное сохранение** — файлы сохраняются с slug-именем и группируются по разделам: `TestCasesTS/<section>/<timestamp>_<name>.ts`.
- **Гибкая конфигурация** — `config.json` с поддержкой кастомного системного промпта и плейсхолдера `{{context}}` для описания приложения.

## Установка

Убедитесь что установлен [Go](https://go.dev/) (1.23+).

```bash
git clone https://github.com/mirshaandrey/conveyor.git
cd conveyor
go build -o conveyor .
```

Для локальной генерации установите [Ollama](https://ollama.ai/) и скачайте модель:

```bash
ollama run qwen2.5-coder:7b
```

Для облачной генерации через Gemini — получите ключ API в [Google AI Studio](https://aistudio.google.com/).

## Конфигурация (`config.json`)

Создайте `config.json` рядом с исполняемым файлом — программа загрузит его автоматически:

```json
{
  "provider": "ollama",
  "gemini_api_key": "",
  "gemini_model": "gemini-2.5-flash",
  "ollama_url": "http://localhost:11434",
  "ollama_model": "qwen2.5-coder:7b",
  "context": "React SPA, baseURL: https://app.example.com, JWT авторизация",
  "review_mode": false,
  "max_retries": 2
}
```

> ⚠️ Добавьте `config.json` в `.gitignore` — файл содержит API ключ.

| Поле | Описание |
| :--- | :--- |
| `provider` | Провайдер по умолчанию: `ollama` или `gemini` |
| `gemini_api_key` | Ключ Google Gemini API |
| `gemini_model` | Модель Gemini (дефолт: `gemini-2.5-flash`) |
| `ollama_url` | Адрес сервера Ollama (дефолт: `http://localhost:11434`) |
| `ollama_model` | Модель Ollama (дефолт: `mistral-small3.2:latest`) |
| `context` | Описание приложения — подставляется в `{{context}}` системного промпта |
| `system_prompt` | Переопределить встроенный промпт. Поддерживает `{{context}}` |
| `review_mode` | Включить второй проход ревью для всех запусков |
| `max_retries` | Число повторов при ошибке валидации (дефолт: `2`) |

**Приоритет настроек:** CLI флаги > `GEMINI_API_KEY` (env) > `config.json` > дефолты.

## Формат тест-кейсов (`cases.json`)

Рекомендуемый формат для пакетной генерации. Поддерживает группировку по разделам и подсказки по селекторам:

```json
{
  "cases": [
    {
      "section": "auth",
      "name": "Вход в аккаунт",
      "task": "Авторизация с корректными учётными данными — успешный вход и редирект на главную",
      "prompt_hint": "Страница: /login. Поля: data-testid=email-input, data-testid=password-input. Кнопка: data-testid=login-button"
    },
    {
      "section": "cart",
      "name": "Добавление товара в корзину",
      "task": "Нажать Add to Cart — товар появляется в корзине с правильным количеством",
      "prompt_hint": "Кнопка: data-testid=add-to-cart. Счётчик: data-testid=cart-count"
    }
  ]
}
```

| Поле | Обязательное | Описание |
| :--- | :---: | :--- |
| `section` | ✓ | Раздел — создаёт подпапку `TestCasesTS/<section>/` |
| `name` | ✓ | Короткое имя — отображается в трекере, формирует имя файла |
| `task` | ✓ | Полное описание — уходит в промпт модели |
| `prompt_hint` | — | Подсказка: конкретные `data-testid`, URL, особенности страницы |

## Использование

### Интерактивный режим

```bash
./conveyor
```

Программа спросит провайдера и модель, затем перейдёт в режим ожидания ввода.

### Одиночный тест-кейс

```bash
./conveyor -task "Авторизация с неверным паролем: ожидается сообщение об ошибке"
```

### Пакетный режим — JSON (рекомендуется)

```bash
./conveyor -cases-file cases.json
```

Файлы сохраняются в `TestCasesTS/<section>/<timestamp>_<name>.ts`.

### Пакетный режим — TXT

```bash
./conveyor -tasks-file cases.txt
```

Простой список задач, по одной на строку. Файлы сохраняются в `TestCasesTS/`.

### Режим Generator → Reviewer

Второй LLM-проход проверяет и исправляет каждый сгенерированный тест:

```bash
./conveyor -cases-file cases.json -review
```

Или включить постоянно через `config.json`: `"review_mode": true`.

### Использование Gemini

```bash
./conveyor -provider=gemini -key="AIzaSy..." -cases-file cases.json
```

### Без автосохранения (вывод в консоль)

```bash
./conveyor -task "..." -no-save
```

## Доступные флаги

| Флаг | По умолчанию | Описание |
| :--- | :--- | :--- |
| `-provider` | `""` | Провайдер LLM: `ollama` или `gemini` |
| `-key` | `""` | API ключ Gemini (или `GEMINI_API_KEY`) |
| `-model` | `""` | Имя модели (зависит от провайдера) |
| `-config` | `config.json` | Путь к файлу конфигурации |
| `-task` | `""` | Одиночный тест-кейс |
| `-tasks-file` | `""` | Файл со списком задач построчно (`.txt`) |
| `-cases-file` | `""` | Файл тест-кейсов в JSON формате (`.json`) |
| `-review` | `false` | Включить второй проход ревью |
| `-out` | `""` | Путь для сохранения результата (одиночный режим) |
| `-no-save` | `false` | Не сохранять файлы автоматически |
| `-url` | `http://localhost:11434` | Адрес сервера Ollama |

## Структура выходных файлов

```
TestCasesTS/
  auth/
    2026-04-01_12-00-00_vkhod_v_akkaunt.ts
    2026-04-01_12-00-15_registratsiya.ts
  cart/
    2026-04-01_12-00-30_dobavlenie_tovara.ts
  checkout/
    2026-04-01_12-00-45_oformlenie_zakaza.ts
```

При использовании `-tasks-file` (`.txt`) все файлы сохраняются в корень `TestCasesTS/`.

## Структура проекта

| Файл | Описание |
| :--- | :--- |
| `main.go` | CLI флаги, выбор провайдера, одиночный и интерактивный режимы |
| `batch.go` | `BatchConfig`, `generateOne` (retry + ревью), `runBatch`, `runBatchCases` |
| `prompt.go` | Системные промпты, `buildCasePrompt`, `cleanCodeBlock`, `validateContent` |
| `ollama.go` | Запросы к Ollama API, стриминг, выбор модели |
| `gemini.go` | Запросы к Gemini API, SSE-стриминг |
| `tracker.go` | Терминальный UI: прогресс-бар, статусы задач, итоговая сводка |
| `files.go` | Структура `TestCase`, парсинг JSON/TXT, slug-генерация, сохранение |
| `config.go` | Структура `Config`, `loadConfig` |

## Contributing

Будем рады Pull Requests! Нашли баг или есть идея — создайте Issue.
