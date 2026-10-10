import "./globals.css";

export const metadata = {
  title: "Axiom — Client Dashboard",
  description: "M12.3 multi-tenant dashboard — Next 16 + shadcn + Better Auth",
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="fr">
      <body className="min-h-screen bg-[var(--color-background)] text-[var(--color-foreground)] antialiased">
        {children}
      </body>
    </html>
  );
}
