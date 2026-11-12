import { createContext, ReactNode, useContext, useState } from 'react';

/**
 * PageContext describes the functional context of the current page/resource.
 * Used by the Harness Router to dynamically resolve the assistant context profile.
 */
export interface PageContext {
  page: string;
  resource_id?: string;
}

interface AssistantContextType {
  isOpen: boolean;
  setIsOpen: (open: boolean) => void;
  pageContext: PageContext | null;
  setPageContext: (ctx: PageContext | null) => void;
}

const AssistantContext = createContext<AssistantContextType | undefined>(undefined);

export function AssistantProvider({ children }: { children: ReactNode }) {
  const [isOpen, setIsOpen] = useState(false);
  const [pageContext, setPageContext] = useState<PageContext | null>(null);

  return (
    <AssistantContext.Provider value={{ isOpen, setIsOpen, pageContext, setPageContext }}>
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
