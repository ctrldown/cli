import { atom } from 'nanostores';

// Exporting this store via Module Federation allows the Shell App or other Microfrontends
// to share state dynamically without complex Redux setups or context providers.
export const counterStore = atom<number>(0);

export const incrementCounter = () => {
  counterStore.set(counterStore.get() + 1);
};
