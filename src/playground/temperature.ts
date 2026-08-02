export function celsiusToFahrenheit(celsius: number): number {
    return celsius * 9 / 5 + 32 
}

export function formatFullName(first: string, last: string) {
    return `${last}, ${first}`
}