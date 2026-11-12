import { createContext, ReactNode, useContext, useState } from 'react';

interface AssistantContextType {
  isOpen: boolean;
  setIsOpen: (open: boolean) => void;
}

const AssistantContext = createContext<AssistantContextType | undefined>(undefined);

export function AssistantProvider({ children }: { children: ReactNode }) {
  const [isOpen, setIsOpen] = useState(false);

  return (
    <AssistantContext.Provider value={{ isOpen, setIsOpen }}>
      {children}
    </AssistantContext.Provider>
  );
}

export function useAssistant() {
  const context = useContext(AssistantContext);
  if (!context) {
    throw new Error('useAssistant must be used within AssistantProvider');
  }
  return context;
}
