import { createContext, ReactNode, useCallback, useContext, useEffect, useRef, useState } from 'react';

/**
 * PageContext describes the functional context of the current page/resource.
 * Used by the Harness Router to dynamically resolve the assistant context profile.
 */
export interface PageContext {
  page: string;
  resource_id?: string;
}

// A purchase or a destruction the model asked for: nothing runs until the person confirms.
export type ActionState = 'pending' | 'busy' | 'done' | 'cancelled' | 'failed' | 'expired';
export type Action = { id: string; tool: string; title: string; recap: string; state?: ActionState; result?: string };
export type UsedTool = { name: string; title: string; write?: boolean; is_error?: boolean };
export type Msg = { role: 'user' | 'assistant'; content: string; tools?: UsedTool[]; actions?: Action[]; note?: boolean };

type MsgUpdate = Msg[] | ((all: Msg[]) => Msg[]);

interface AssistantContextType {
  isOpen: boolean;
  setIsOpen: (open: boolean) => void;
  pageContext: PageContext | null;
  setPageContext: (ctx: PageContext | null) => void;
  // The conversation lives here, above the pages: it follows the person from
  // page to page, and between the side panel and the assistant's page.
  conversationId: string;
  msgs: Msg[];
  setMsgs: (update: MsgUpdate) => void;
  newConversation: () => void;
  openConversation: (id: string) => Promise<boolean>;
  // restore reopens, once, the conversation of the last visit.
  restore: () => void;
}

const AssistantContext = createContext<AssistantContextType | undefined>(undefined);

const storageKey = 'myinfra-assistant-conversation';

function remembered(): string | null {
  try {
    return localStorage.getItem(storageKey);
  } catch {
    return null;
  }
}

function remember(id: string) {
  try {
    localStorage.setItem(storageKey, id);
  } catch {
    /* the conversation still lives for this visit */
  }
}

export function AssistantProvider({ children }: { children: ReactNode }) {
  const [isOpen, setIsOpen] = useState(false);
  const [pageContext, setPageContext] = useState<PageContext | null>(null);
  const [conversationId, setConversationId] = useState(() => remembered() ?? crypto.randomUUID());
  const [msgs, setMsgsState] = useState<Msg[]>([]);
  // A change made here is kept on the server; one read from it is not sent back.
  const dirty = useRef(false);
  const restored = useRef(false);

  const setMsgs = useCallback((update: MsgUpdate) => {
    dirty.current = true;
    setMsgsState(update);
  }, []);

  useEffect(() => {
    remember(conversationId);
  }, [conversationId]);

  useEffect(() => {
    if (!dirty.current || msgs.length === 0) return;
    dirty.current = false;
    void fetch(`/api/assistant/conversations/${encodeURIComponent(conversationId)}`, {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ messages: msgs }),
    }).catch(() => undefined);
  }, [msgs, conversationId]);

  const openConversation = useCallback(async (id: string) => {
    try {
      const r = await fetch(`/api/assistant/conversations/${encodeURIComponent(id)}`);
      if (!r.ok) return false;
      const c: { messages?: Msg[] } = await r.json();
      dirty.current = false;
      setConversationId(id);
      setMsgsState(Array.isArray(c.messages) ? c.messages : []);
      return true;
    } catch {
      return false;
    }
  }, []);

  const newConversation = useCallback(() => {
    dirty.current = false;
    setConversationId(crypto.randomUUID());
    setMsgsState([]);
  }, []);

  const restore = useCallback(() => {
    if (restored.current) return;
    restored.current = true;
    const id = remembered();
    if (id) void openConversation(id);
  }, [openConversation]);

  return (
    <AssistantContext.Provider value={{ isOpen, setIsOpen, pageContext, setPageContext, conversationId, msgs, setMsgs, newConversation, openConversation, restore }}>
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
