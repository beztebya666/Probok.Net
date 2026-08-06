import Link from "next/link";

export default function OfflinePage() {
  return (
    <main className="offline-page" id="main-content">
      <div className="offline-card">
        {/* Painted through CSS so this static page can follow the theme without
            becoming a client component just to pick a file name. */}
        <span className="offline-logo" aria-hidden="true" />
        <p className="eyebrow">Пробок.Нет</p>
        <h1>Нет подключения</h1>
        <p>Для поиска маршрута нужны актуальные данные. Проверьте сеть и попробуйте снова.</p>
        <Link className="button button-primary" href="/">Повторить</Link>
      </div>
    </main>
  );
}
