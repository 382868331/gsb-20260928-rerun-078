import { Component, type ReactNode } from "react";

interface State {
  error: Error | null;
}

// Last-resort guard so a malformed/legacy check payload renders a
// readable message instead of blanking the whole page.
export class ErrorBoundary extends Component<{ children: ReactNode }, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  render() {
    if (this.state.error) {
      return (
        <div className="fatal">
          <h3>页面渲染遇到问题，但数据未丢失</h3>
          <pre>{String(this.state.error.message || this.state.error)}</pre>
          <button onClick={() => this.setState({ error: null })}>重试渲染</button>
        </div>
      );
    }
    return this.props.children;
  }
}
