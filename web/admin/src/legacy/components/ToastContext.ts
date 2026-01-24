import { createContext } from 'react';

type ToastType = 'success' | 'error' | 'warning';

export interface ToastContextType {
  showToast: (type: ToastType, message: string) => void;
}

export const ToastContext = createContext<ToastContextType | null>(null);
