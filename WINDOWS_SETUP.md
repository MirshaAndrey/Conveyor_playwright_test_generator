# Запуск Conveyor на Windows 11

## Требования

| Программа | Версия | Ссылка |
|:---|:---|:---|
| Go | 1.22+ | https://go.dev/dl/ |
| Node.js | 18+ | https://nodejs.org/ |
| Git | любая | https://git-scm.com/ |
| Ollama (если локально) | любая | https://ollama.ai/ |

---

## Шаг 1 — Установка Go

1. Откройте https://go.dev/dl/
2. Скачайте `go1.22.x.windows-amd64.msi`
3. Запустите установщик, оставьте все настройки по умолчанию
4. Откройте новый **PowerShell** и проверьте:

```powershell
go version
# Ожидаемый вывод: go version go1.22.x windows/amd64
```

---

## Шаг 2 — Установка Node.js

1. Откройте https://nodejs.org/
2. Скачайте LTS версию (18+ или 20+)
3. Запустите установщик, включите опцию **"Add to PATH"**
4. Проверьте:

```powershell
node --version   # v20.x.x
npm --version    # 10.x.x
```

---

## Шаг 3 — Сборка Conveyor

Откройте **PowerShell** или **Windows Terminal** в папке с исходниками:

```powershell
# Перейдите в папку с файлами
cd C:\путь\к\conveyor

# Соберите бинарник
go build -o conveyor.exe .

# Проверьте что собралось
.\conveyor.exe --help
```

---

## Шаг 4 — Настройка Node.js зависимостей

Выполните в той же папке где лежит `conveyor.exe`:

```powershell
# Установите Playwright
npm install playwright

# Установите браузер Chromium для сканера страниц
npx playwright install chromium
```

После этого в папке появится `node_modules\` — Conveyor найдёт её автоматически.

---

## Шаг 5 — Настройка провайдера

### Вариант A: Ollama (локально, рекомендуется)

1. Скачайте и установите с https://ollama.ai/
2. Откройте PowerShell и запустите сервер:

```powershell
ollama serve
```

3. В отдельном окне скачайте модель:

```powershell
ollama pull qwen2.5-coder:7b
```

4. Проверьте что модель доступна:

```powershell
ollama list
```

### Вариант B: Google Gemini API

1. Откройте https://aistudio.google.com/
2. Создайте API ключ
3. Добавьте в `config.json`:

```json
{
  "provider": "gemini",
  "gemini_api_key": "AIzaSy...",
  "gemini_model": "gemini-2.5-flash"
}
```

**Или** задайте через переменную окружения:

```powershell
$env:GEMINI_API_KEY = "AIzaSy..."
```

---

## Шаг 6 — Настройка config.json

Откройте `config.json` в любом редакторе (Notepad, VS Code) и заполните:

```json
{
  "provider": "ollama",
  "gemini_api_key": "",
  "gemini_model": "gemini-2.5-flash",
  "ollama_url": "http://localhost:11434",
  "ollama_model": "qwen2.5-coder:7b",
  "context": "Описание вашего приложения: стек, baseURL, особенности",
  "review_mode": false,
  "max_retries": 2
}
```

> ⚠️ Не добавляйте `config.json` в Git если он содержит API ключ.

---

## Шаг 7 — Первый запуск

### Интерактивный режим

```powershell
.\conveyor.exe
```

Программа спросит провайдера и модель, затем ждёт ввода задачи.

### Пакетная генерация из JSON

```powershell
.\conveyor.exe -cases-file cases.json
```

### Агентный режим (генерация + запуск + починка)

```powershell
.\conveyor.exe -agent -cases-file cases.json
```

### С ревью

```powershell
.\conveyor.exe -cases-file cases.json -review
```

---

## Структура папки после первого запуска

```
conveyor\
├── conveyor.exe          ← собранный бинарник
├── config.json           ← настройки
├── cases.json            ← тест-кейсы
├── package.json          ← Node.js зависимости
├── playwright_config.ts  ← конфиг для запуска тестов
├── node_modules\         ← создаётся после npm install
└── TestCasesTS\          ← сюда сохраняются сгенерированные тесты
    └── auth\
        └── 2026-04-12_vkhod_v_akkaunt.ts
```

---

## Запуск сгенерированных тестов вручную

```powershell
# Запустить все тесты
npx playwright test

# Запустить конкретный файл
npx playwright test TestCasesTS\auth\vkhod_v_akkaunt.ts

# Открыть HTML-отчёт после запуска
npx playwright show-report
```

---

## Решение типичных проблем

### `go: command not found`

Go не добавлен в PATH. Откройте **новое** окно PowerShell после установки Go. Если не помогает:

```powershell
# Проверьте наличие Go
Test-Path "C:\Program Files\Go\bin\go.exe"

# Добавьте вручную в текущую сессию
$env:PATH += ";C:\Program Files\Go\bin"
```

### `ollama: command not found`

Ollama не добавлена в PATH. Перезапустите PowerShell после установки. Или запускайте напрямую:

```powershell
& "C:\Users\$env:USERNAME\AppData\Local\Programs\Ollama\ollama.exe" serve
```

### `playwright: command not found` при сканировании

```powershell
npm install playwright
npx playwright install chromium
```

### Ошибка `node_modules not found`

Убедитесь что `npm install playwright` запущен в **той же папке**, где лежит `conveyor.exe`.

### Кириллица отображается как `????` в терминале

```powershell
# Установите UTF-8 кодировку в текущей сессии
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
chcp 65001
```

Чтобы не вводить каждый раз, добавьте в профиль PowerShell:

```powershell
notepad $PROFILE
# Добавьте строку: [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
```

### Ollama недоступна при запуске агента

Убедитесь что `ollama serve` запущен в отдельном окне PowerShell **перед** запуском Conveyor.

### `Error: browserType.launch: Executable doesn't exist`

```powershell
npx playwright install chromium
```

---

## Кросс-компиляция (для других ОС из Windows)

```powershell
# Linux
$env:GOOS="linux"; $env:GOARCH="amd64"; go build -o conveyor-linux .

# macOS Intel
$env:GOOS="darwin"; $env:GOARCH="amd64"; go build -o conveyor-mac .

# macOS Apple Silicon
$env:GOOS="darwin"; $env:GOARCH="arm64"; go build -o conveyor-mac-arm .

# Сброс переменных
Remove-Item Env:GOOS; Remove-Item Env:GOARCH
```

---

## Рекомендуемая модель для Windows

На большинстве машин с 8GB+ RAM рекомендуется `qwen2.5-coder:7b` — хорошее соотношение качества и скорости. На 16GB+ можно попробовать `qwen2.5-coder:14b`.

```powershell
ollama pull qwen2.5-coder:7b
.\conveyor.exe -cases-file cases.json -model qwen2.5-coder:7b
```
