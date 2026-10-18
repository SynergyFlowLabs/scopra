import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "Scopra Dashboard",
  description: "Securely pair and manage your Scopra machines and workspaces.",
  robots: { index: false, follow: false },
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
