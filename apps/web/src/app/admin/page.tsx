import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { AdminDashboard } from "@/components/admin-dashboard";
import { getRuntimeConfig } from "@/lib/runtime-config";

export const metadata: Metadata = {
  title: "Operations",
  robots: { index: false, follow: false, noarchive: true },
};

export const dynamic = "force-dynamic";

export default function AdminPage() {
  // Off unless the deployment asks for it: an operations console is not part of
  // what an ordinary visitor should be able to reach.
  if (!getRuntimeConfig().adminEnabled) notFound();
  return <AdminDashboard />;
}
