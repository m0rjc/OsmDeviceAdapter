import { createContext } from 'react';

export type ToastType = 'success' | 'error' | 'warning' | 'info';

export interface ToastContextType {
  showToast: (type: ToastType, message: string) => void;
}

export const ToastContext = createContext<ToastContextType | null>(null);
