import { betterAuth } from "better-auth";
import { bearer } from "better-auth/plugins";

// Auth is backed by the Axiom Engine session endpoint rather than a standalone DB.
// The client sends the session cookie or bearer; the Engine resolves the user.
export const auth = betterAuth({
  plugins: [bearer()],
  session: { cookieCache: { enabled: true, maxAge: 5 * 60 } },
  baseURL: process.env.NEXT_PUBLIC_ENGINE_URL || "https://axiom.eurinhash.com/api",
  secret: process.env.BETTER_AUTH_SECRET || "dev-secret-change-in-production",
});

export type Session = typeof auth.$Infer.Session.session;
