import { Component, type ErrorInfo, type ReactNode } from "react";

type ErrorBoundaryProps = {
  children: ReactNode;
  name?: string;
};

type ErrorBoundaryState = {
  hasError: boolean;
  error: Error | null;
};

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { hasError: false, error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    if (import.meta.env.DEV) console.error("Synora render error", error, info.componentStack);
  }

  reload = () => window.location.reload();

  goToDashboard = () => {
    window.location.assign("/");
  };

  render() {
    if (!this.state.hasError) return this.props.children;
    return (
      <section className="page-placeholder error-boundary-card" role="alert">
        <h2>Une erreur d’affichage est survenue.</h2>
        <p>Recharge la page ou retourne au tableau de bord.</p>
        <div className="page-placeholder-actions">
          <button type="button" className="primary-button" onClick={this.reload}>Recharger</button>
          <button type="button" className="secondary-button" onClick={this.goToDashboard}>Retour tableau de bord</button>
        </div>
        {import.meta.env.DEV && this.state.error && (
          <details>
            <summary>Détails techniques</summary>
            <pre>{this.state.error.stack || this.state.error.message}</pre>
          </details>
        )}
      </section>
    );
  }
}
