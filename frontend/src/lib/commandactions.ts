// Page-contextual command-palette actions. Pages register what makes
// sense where the user is (e.g. Resolve on an issue detail); the palette
// snapshots the registry when it opens. Registration is explicit and
// cleared on page destroy so actions never leak across views.
export interface ContextAction {
  label: string;
  keywords?: string[];
  run: () => void;
}

let actions: ContextAction[] = [];

export function setContextActions(next: ContextAction[]): void {
  actions = next;
}

export function getContextActions(): ContextAction[] {
  return actions;
}

export function clearContextActions(): void {
  actions = [];
}
