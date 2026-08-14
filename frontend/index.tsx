import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App';
import './index.css';

const rootElement = document.getElementById('root'); /* rootElement 表示rootElement。 */
if (!rootElement) {
  throw new Error("Could not find root element to mount to");
}

const root = ReactDOM.createRoot(rootElement); /* root 表示root。 */
root.render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
