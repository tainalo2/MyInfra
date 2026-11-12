import { IconButton } from "./ui";
import { MessageCircle } from "lucide-react";
import { useAssistant } from "./assistant-context";

export function AssistantToggle() {
  const { isOpen, setIsOpen } = useAssistant();
  
  return (
    <IconButton
      icon={MessageCircle}
      label="Assistant IA"
      onClick={() => setIsOpen(!isOpen)}
      className={isOpen ? "text-ink" : "text-muted"}
      title={isOpen ? "Fermer l'assistant" : "Ouvrir l'assistant"}
    />
  );
}
