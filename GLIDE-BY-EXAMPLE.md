# Glide By Example
Glide examples, starting from simple and working up, showing idiomatic Glide code.

Every fenced block here is run against the interpreter by `go test`
(see `glide/doc_examples_test.go`). Comment lines at the *end* of a
block are checked: `// error: …` means the program must fail with
exactly that error; other trailing comments are the expected output.

As simple as it gets. Hello World !!!
No imports needed. the entry point to any glide program has to be the main() function
```rust
fn main() {
    println("Hello, Glide!")
}
// Hello, Glide!
```

With a variable. Glide is strongly typed, but type is inferred by a literal.

There is really only 2 forms of print. `print` and `println` (`ln` just adds a new line at the end)

No printf. Everything you need is interpolated within `print`/`println`
(eprint/eprintln work the same way, but output to stderr rather than stdout)
```rust
fn main() {
    let name = "Glide"
    println("Hello {name}!")
}
```

You can be explicit if you want. Sometimes it's required, but not here.
```rust
fn main() {
    let name: String = "Glide"
    println("Hello {name}!")
}
```

Variables are immutable by default. They can't be changed after creation.
```rust
fn main() {
    let count = 1
    count = count + 1
    println(count)
}
// error: line 3: cannot mutate through immutable binding "count" (declare it with `let mut`)
```

Make variables mutable (changeable) with the `mut` keyword.
Add in the standard shortcut for adding to a variable
```rust
fn main() {
    let mut count = 1
    count += 1
    println(count)
}
// 2
```

You can reassign (or shadow) a variable within the same scope, but not in another scope (shown later)
```rust
fn main() {
    let count = 1
    let count = count + 2
    println(count)
    let count = "three"
    println(count)
}
// 3
// three
```

## Functions
```rust
fn main() {
	let name = "Glide"
	greet()
}

// greet doesn't accept or return any values.
fn greet() {
	print("Hello ")
	println("Glide!")
}
// Hello Glide!
```

Pass a variable to a function.
The functions can be in any order. You can call a function not yet defined.

```rust
fn main() {
	greet("Glide")
}
// Function definition has to contain the type of parameter it receives
fn greet(name: String) {
	print("Hello ")
	println("{name}!")
}
```

## Closures

`fn` declarations are top level only. But a closure is an *expression* —
a function you can create anywhere, bind to a variable, and pass around.
Written with pipes around the parameters: `|x|`, or `||` for none.

A closure can see the variables of the scope it was created in.
```rust
fn main() {
	let greeting = "Hello"
	let greet = |name| {
		println("{greeting} {name}!")
	}
	greet("Glide")
	greet("closures")
}
// Hello Glide!
// Hello closures!
```

A one-expression body needs no braces, and its result is returned.
Captured variables are shared, not copied: mutating a captured
`let mut` is visible outside the closure.
```rust
fn main() {
	let double = |n| n * 2
	println(double(21))

	let mut total = 0
	let add = |n| { total += n }
	add(3)
	add(4)
	println(total)
}
// 42
// 7
```
