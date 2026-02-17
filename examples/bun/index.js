import { say } from "cowsay";

const text = process.env.COWSAY_TEXT || "Hello from bun + cowsay";

console.log(say({ text }));
