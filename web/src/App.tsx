import { Route, Switch } from 'wouter';
import RepositoryListPage from './pages/RepositoryListPage';
import RepoDashboardPage from './pages/RepoDashboardPage';
import AuthorMergePage from './pages/AuthorMergePage';

export default function App() {
  return (
    <Switch>
      <Route path="/" component={RepositoryListPage} />
      <Route path="/repos/:id">
        {(params) => <RepoDashboardPage repoId={Number(params.id)} />}
      </Route>
      <Route path="/repos/:id/authors">
        {(params) => <AuthorMergePage repoId={Number(params.id)} />}
      </Route>
      <Route>
        <main className="app-shell">
          <h1>Page not found</h1>
          <p><a href="/">Go to repositories</a></p>
        </main>
      </Route>
    </Switch>
  );
}
