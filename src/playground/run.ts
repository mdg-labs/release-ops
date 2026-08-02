import { celsiusToFahrenheit, formatFullName } from "./temperature.js";

const firstNames = ["Michael", "Barbara", "David"]

for (const firstName of firstNames) {
    console.log(firstName)
}

console.log("Full Name:", formatFullName("Michael", "Guggenbichler"))
console.log("Fahrenheit:", celsiusToFahrenheit(38))