interface Book {
    title: string;
    author: string;
    year: number;
}

const books: Book[] = [
    {title: "Maze Runner - Im Labyrinth", author: "James Dashner", year: 2009},
    {title: "Eragon - Das Vermächtnis der Drachenreiter", author: "Christopher Paolini", year: 2002},
    {title: "Ready Player One", author: "Ernest Cline", year: 2011},
    {title: "Harry Potter und der Stein der Weisen", author: "Joanne K. Rowling", year: 1997}
]

export function findOldestBook(books: Book[]) {
    let oldestBook: Book | undefined = undefined;
    for (const book of books) {
        if (oldestBook === undefined || book.year < oldestBook.year) {
            oldestBook = book;
        }
    }
    return oldestBook;
}