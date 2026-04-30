import Router, { route } from "preact-router";
import { LoginPage } from "../pages/login";
import { SessionsPage } from "../pages/sessions";
import { TerminalPage } from "../pages/terminal";
import { HelpPage } from "../pages/help";
import { AuthGuard } from "./AuthGuard";
import { logout } from "../api/endpoints";
import { accessToken, clearAuth } from "../auth/store";

const LoginRoute = (_props: { path?: string }) => (
  <LoginPage
    onSuccess={() => route("/sessions", true)}
    onHelp={() => route("/help")}
  />
);

const SessionsRoute = (_props: Record<string, unknown>) => (
  <AuthGuard>
    <SessionsPage
      onOpen={(id) => route(`/terminal/${id}`)}
      onHelp={() => route("/help")}
      onLogout={async () => {
        await logout();
        clearAuth();
        route("/", true);
      }}
    />
  </AuthGuard>
);

const TerminalRoute = (props: { path?: string; sessionId?: string }) => (
  <AuthGuard>
    <TerminalPage
      sessionId={props.sessionId ?? ""}
      onBack={() => route("/sessions")}
    />
  </AuthGuard>
);

const HelpRoute = (_props: { path?: string }) => (
  <HelpPage onBack={() => route(accessToken.value ? "/sessions" : "/", true)} />
);

export function AppRouter() {
  return (
    <Router>
      <LoginRoute path="/" />
      <HelpRoute path="/help" />
      <SessionsRoute path="/sessions" />
      <TerminalRoute path="/terminal/:sessionId" />
    </Router>
  );
}
