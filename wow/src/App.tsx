import React from 'react';
import { useStore } from '@nanostores/react';
import { counterStore, incrementCounter } from './store';

const App: React.FC = () => {
  const count = useStore(counterStore);

  return (
    <div style={{ padding: '20px', border: '2px solid #00d8ff', borderRadius: '8px', backgroundColor: '#f0f4f8' }}>
      <h2 style={{ color: '#333' }}>Base React Rspack Microfrontend</h2>
      <p style={{ color: '#555' }}>This is a pure UI component (Naked Microfrontend). No server, no infrastructure.</p>
      <div style={{ marginTop: '15px' }}>
        <strong style={{ color: '#222' }}>Global State (Nanostores) Count: {count}</strong>
        <br />
        <button 
          onClick={incrementCounter} 
          style={{ 
            marginTop: '10px', 
            padding: '8px 16px', 
            backgroundColor: '#00d8ff', 
            border: 'none', 
            borderRadius: '4px',
            cursor: 'pointer',
            fontWeight: 'bold',
            color: '#fff'
          }}
        >
          Increment
        </button>
      </div>
    </div>
  );
};

export default App;
