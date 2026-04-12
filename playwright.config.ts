import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  // Папка с тестами — сюда Conveyor сохраняет все сгенерированные файлы
  testDir: './TestCasesTS',

  // Подхватывает все .ts файлы, включая сгенерированные (без суффикса .spec)
  testMatch: '**/*.ts',

  // Исключаем POM-файлы страниц — они не содержат тестов, только классы
  testIgnore: '**/pom/pages/**',

  // Параллельный запуск файлов
  fullyParallel: true,

  // Запрет test.only в CI
  forbidOnly: !!process.env.CI,

  // Повторы при падении: 1 локально, 2 на CI
  retries: process.env.CI ? 2 : 1,

  // Количество воркеров
  workers: process.env.CI ? 1 : undefined,

  // Репортер: список строк в консоли + HTML для детального просмотра
  reporter: [['line'], ['html', { open: 'never' }]],

  use: {
    // Таймаут одного действия (клик, fill, expect)
    actionTimeout: 10_000,

    // Таймаут навигации
    navigationTimeout: 30_000,

    // Скриншот только при падении теста
    screenshot: 'only-on-failure',

    // Trace при первом retry — помогает разобраться в причине падения
    trace: 'on-first-retry',

    // Игнорировать ошибки HTTPS (полезно для локальных стендов)
    ignoreHTTPSErrors: true,
  },

  // Таймаут одного теста целиком
  timeout: 60_000,

  // Таймаут expect() по умолчанию
  expect: {
    timeout: 10_000,
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },

    // Раскомментируй для кросс-браузерного тестирования:
    // { name: 'firefox', use: { ...devices['Desktop Firefox'] } },
    // { name: 'webkit',  use: { ...devices['Desktop Safari']  } },
  ],
});
