import Link from "next/link";

export default function NotFound() {
  return (
    <main className="fatal-error" id="main-content">
      <p className="eyebrow">404 · Пробок.Нет</p>
      <h1>Страница не найдена</h1>
      <Link className="button button-primary" href="/">К маршрутам</Link>
    </main>
  );
}
