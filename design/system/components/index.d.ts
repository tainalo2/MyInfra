// MyInfra components — types from tainalo2/MyInfra web/src/ui.tsx and web/src/kit.tsx (main@a1a9341).
// The bundle assigns window.MyInfra; it also carries `setLang(lang)` and a few lucide `icons`.
import type { ButtonHTMLAttributes, ComponentType, InputHTMLAttributes, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes, JSX } from "react";

/** A lucide-react icon component (16px, stroke 1.5 when drawn through Icon). */
export type LucideIcon = ComponentType<{ size?: number; strokeWidth?: number; className?: string }>;

export interface IconProps { icon: LucideIcon; className?: string }
export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> { /** default "primary" */ variant?: "primary" | "secondary" | "danger" }
export interface TextButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> { tone?: "danger" }
export interface IconButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> { icon: LucideIcon; /** aria-label and title */ label: string }
export interface ActionsMenuProps { /** accessible name of the "…" button */ label: string; /** MenuItem elements */ children: ReactNode }
export interface MenuItemProps extends ButtonHTMLAttributes<HTMLButtonElement> { tone?: "danger" }
export interface FieldProps { label: string; hint?: ReactNode; /** one control: Input, Select or TextArea */ children: ReactNode }
export interface GroupProps { label: string; hint?: ReactNode; children: ReactNode }
export type InputProps = InputHTMLAttributes<HTMLInputElement>;
export type SelectProps = SelectHTMLAttributes<HTMLSelectElement>;
export type TextAreaProps = TextareaHTMLAttributes<HTMLTextAreaElement>;
export interface ErrorTextProps { children: ReactNode }
export interface StatusBadgeProps { /** "running" | "ready" | "linked" | "done" | "deploying" | "updating" | "pending" | "pending_dns" | "preparing" | "removing" | "failed" | "unreachable" | "lost" | "gone" | "paused" | "cancelled" | "unpublished" */ status?: string }
export interface HealthDotProps { status?: string }
export interface SpinnerProps {}
export interface SkeletonProps { /** Tailwind size classes, default "h-4 w-32" */ className?: string }
export interface LoadingLinesProps { /** default 3 */ lines?: number; label?: string }
export interface FoldProps { title: string; children: ReactNode; defaultOpen?: boolean }
export interface TabsProps<K extends string = string> { tabs: { key: K; label: string; mark?: boolean }[]; active: K; onChange: (key: K) => void; label: string }
export interface CopyButtonProps { value: string; label?: string }
export interface SheetProps { title: ReactNode; onClose: () => void; busy?: boolean; children: ReactNode; wide?: boolean }
export interface ConfirmDialogProps { title: ReactNode; children?: ReactNode; confirmLabel: string; /** typed to confirm what cannot be undone */ phrase?: string; phraseLabel?: string; danger?: boolean; onConfirm: () => Promise<unknown> | unknown; onClose: () => void }
export interface PageTitleProps { title: string; icon?: LucideIcon; mark?: ReactNode; action?: ReactNode }
export interface SectionTitleProps { children: ReactNode }

export declare function Icon(props: IconProps): JSX.Element;
export declare function Button(props: ButtonProps): JSX.Element;
export declare function TextButton(props: TextButtonProps): JSX.Element;
export declare function IconButton(props: IconButtonProps): JSX.Element;
export declare function ActionsMenu(props: ActionsMenuProps): JSX.Element;
export declare function MenuItem(props: MenuItemProps): JSX.Element;
export declare function Field(props: FieldProps): JSX.Element;
export declare function Group(props: GroupProps): JSX.Element;
export declare function Input(props: InputProps): JSX.Element;
export declare function Select(props: SelectProps): JSX.Element;
export declare function TextArea(props: TextAreaProps): JSX.Element;
export declare function ErrorText(props: ErrorTextProps): JSX.Element;
export declare function StatusBadge(props: StatusBadgeProps): JSX.Element;
export declare function HealthDot(props: HealthDotProps): JSX.Element;
export declare function Spinner(props: SpinnerProps): JSX.Element;
export declare function Skeleton(props: SkeletonProps): JSX.Element;
export declare function LoadingLines(props: LoadingLinesProps): JSX.Element;
export declare function Fold(props: FoldProps): JSX.Element;
export declare function Tabs<K extends string>(props: TabsProps<K>): JSX.Element;
export declare function CopyButton(props: CopyButtonProps): JSX.Element;
export declare function Sheet(props: SheetProps): JSX.Element;
export declare function ConfirmDialog(props: ConfirmDialogProps): JSX.Element;
export declare function PageTitle(props: PageTitleProps): JSX.Element;
export declare function SectionTitle(props: SectionTitleProps): JSX.Element;
export declare function setLang(lang: "fr" | "en" | "pt" | "it" | "de"): void;
export declare const icons: Record<"Plus" | "Trash2" | "Copy" | "MoreHorizontal" | "Server" | "Boxes" | "Globe" | "RotateCw" | "Pause" | "Play" | "Pencil" | "Eye" | "Settings" | "Search" | "Download", LucideIcon>;
