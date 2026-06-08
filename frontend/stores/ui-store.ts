import { create } from "zustand";

type UiState = {
  count: number;
  increment: () => void;
  reset: () => void;
};

export const useUiStore = create<UiState>((set) => ({
  count: 0,
  increment: () => set((state) => ({ count: state.count + 1 })),
  reset: () => set({ count: 0 }),
}));
