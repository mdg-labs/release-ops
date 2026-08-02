// convert celsius to fahrenheit
export function celsiusToFahrenheit(celsius: number): number {
    return celsius * 9 / 5 + 32 
}

// convert seperated naming format to one full name
export function formatFullName(first: string, last: string) {
    return `${last}, ${first}`
}