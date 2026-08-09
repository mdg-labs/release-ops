import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { usePersistedPageSize } from "./use-persisted-page-size";

const STORAGE_KEY = "test:page-size";

function createLocalStorageMock(): Storage {
  let store: Record<string, string> = {};

  return {
    get length() {
      return Object.keys(store).length;
    },
    clear() {
      store = {};
    },
    getItem(key: string) {
      return store[key] ?? null;
    },
    key(index: number) {
      return Object.keys(store)[index] ?? null;
    },
    removeItem(key: string) {
      delete store[key];
    },
    setItem(key: string, value: string) {
      store[key] = value;
    },
  };
}

describe("usePersistedPageSize", () => {
  let localStorageMock: Storage;

  beforeEach(() => {
    localStorageMock = createLocalStorageMock();
    vi.stubGlobal("localStorage", localStorageMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("returns the default page size when storage is empty", () => {
    const { result } = renderHook(() => usePersistedPageSize(STORAGE_KEY));

    expect(result.current[0]).toBe(10);
  });

  it("hydrates from localStorage on mount", async () => {
    localStorageMock.setItem(STORAGE_KEY, "25");

    const { result } = renderHook(() => usePersistedPageSize(STORAGE_KEY));

    await waitFor(() => {
      expect(result.current[0]).toBe(25);
    });
  });

  it("persists page size changes to localStorage", async () => {
    const { result } = renderHook(() => usePersistedPageSize(STORAGE_KEY));

    act(() => {
      result.current[1](50);
    });

    await waitFor(() => {
      expect(result.current[0]).toBe(50);
    });
    expect(localStorageMock.getItem(STORAGE_KEY)).toBe("50");
  });

  it("ignores invalid stored values", async () => {
    localStorageMock.setItem(STORAGE_KEY, "999");

    const { result } = renderHook(() => usePersistedPageSize(STORAGE_KEY));

    await waitFor(() => {
      expect(result.current[0]).toBe(10);
    });
  });
});
