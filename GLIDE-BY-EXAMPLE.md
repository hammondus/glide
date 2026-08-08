# Glide By Example
Glide examples, starting from simple and working up, showing idomatic Glide code.

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
(eprint/eprintln work the sameway, but output to stderr rather than stdout)
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

Make variables mutable (changable) with the `mut` keyboaqrd
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
    let count += 2
    println(count)
    let count = "three"
    println(count)
}
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