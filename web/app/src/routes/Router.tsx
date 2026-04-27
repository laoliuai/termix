import Router, { route } from "preact-router";
import { LoginPage } from "../pages/login";
import { SessionsPage } from "../pages/sessions";
import { TerminalPage } from "../pages/terminal";
import { AuthGuard } from "./AuthGuard";
import { logout } from "../api/endpoints";
import { clearAuth } from "../auth/store";

const SessionsRoute = (_props: Record<string, unknown>) => (
  <AuthGuard>
    <SessionsPage
      onOpen={(id) => route(`/terminal/${id}`)}
      onLogout={async () => {
        await logout();
        clearAuth();
        route("/", true);
      }}
    />
  </AuthGuard>
);

const TerminalRoute = (props: { sessionId?: string }) => (
  <AuthGuard>
    <TerminalPage
      sessionId={props.sessionId ?? ""}
      onBack={() => route("/sessions")}
    />
  </AuthGuard>
);

export function AppRouter() {
  return (
    <Router>
      <LoginPage path="/" onSuccess={() => route("/sessions", true)} />
      <SessionsRoute path="/sessions" />
      <TerminalRoute path="/terminal/:sessionId" />
    </Router>
  );
}
