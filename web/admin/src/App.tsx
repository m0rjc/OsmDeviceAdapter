import { Provider } from 'react-redux';
import { store } from './ui/state/store';
import { App as UIApp } from './ui/App';
import './styles.css';

/**
 * Root application component that provides Redux store.
 *
 * This wraps the new UI implementation with the Redux Provider.
 * The actual app logic is in ui/App.tsx.
 */
function App() {
  return (
    <Provider store={store}>
      <UIApp />
    </Provider>
  );
}

export default App;
