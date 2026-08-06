"use client";

export default function ErrorPage({ reset }: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <main className="fatal-error" role="alert" id="main-content">
      <p className="eyebrow">Пробок.Нет</p>
      <h1>Не удалось открыть страницу</h1>
      <p>Попробуйте снова. Данные маршрута не были сохранены в браузере.</p>
      <button className="button button-primary" type="button" onClick={reset}>Повторить</button>
    </main>
  );
}
