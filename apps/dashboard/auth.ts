import NextAuth from "next-auth";
import GitHub from "next-auth/providers/github";

const authSecret = process.env.AUTH_SECRET;

export const { handlers, auth, signIn, signOut } = NextAuth({
  providers: [GitHub],
  trustHost: true,
  secret: authSecret,
  session: {
    strategy: "jwt",
    maxAge: 8 * 60 * 60,
  },
  pages: {
    signIn: "/",
  },
  callbacks: {
    async jwt({ token, account }) {
      if (account?.provider === "github" && /^[0-9]{1,20}$/.test(account.providerAccountId)) {
        token.sub = account.providerAccountId;
      }
      return token;
    },
    async session({ session, token }) {
      if (session.user && typeof token.sub === "string") {
        session.user.id = token.sub;
      }
      return session;
    },
  },
});
