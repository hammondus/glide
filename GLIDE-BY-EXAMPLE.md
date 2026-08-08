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

## If

`if` is an expression — it produces a value, so there is no ternary
operator: `if` *is* the ternary, and it reads the same at two lines or
ten. Conditions take no parentheses; braces are always required.
```rust
fn main() {
    let n = 7
    let sign = if n > 0 { "positive" } else if n < 0 { "negative" } else { "zero" }
    println(sign)
}
// positive
```

The condition must be a Bool. There is no truthiness — `if 1 { }` is
an error, so the classic `if x = 1` / `if 0` bug families don't exist.
```rust
fn main() {
    if 1 { println("never") }
}
// error: line 2: if condition must be Bool, got Int
```

## Loops

One loop keyword: `for`. Three forms — forever, while-style, and
over the elements of anything iterable. `break` and `continue` do
what Go's do, and using them outside a loop is caught before the
program runs.
```rust
fn main() {
    // Forever, until break.
    let mut n = 1
    for {
        n *= 2
        if n > 100 { break }
    }
    println(n)

    // While-style: loop while the condition holds.
    let mut k = 0
    for k < 3 {
        k += 1
    }
    println(k)

    // Over a range (half-open: 0, 1, 2) — with continue to skip.
    for i in 0..3 {
        if i == 1 { continue }
        println(i)
    }
}
// 128
// 3
// 0
// 2
```

## Lists

Lists grow with `push`, which — like all mutation — needs a `mut`
binding. Indexing panics out of bounds: an invalid index is a bug in
the program, not a condition to handle.
```rust
fn main() {
    let mut primes = [2, 3, 5]
    primes.push(7)
    println(primes.len())
    println(primes[3])

    primes[0] = 11
    primes[0] += 2
    println(primes[0])
}
// 4
// 7
// 13
```

`repeat` is the fill constructor: n slots, value named explicitly —
Glide has no zero values, so there is no `make([]int, n)` filling
things in behind your back.
```rust
fn main() {
    let board = ["."].repeat(3)
    println(board.len())
    println(board[2])
}
// 3
// .
```

`sorted` returns a new list and works on any immutable binding;
`sort_by` sorts in place, so it needs `mut`.
```rust
fn main() {
    let xs = [3, 1, 2]
    println(xs.sorted()[0])
    println(xs[0])
}
// 1
// 3
```

## Maps

Map literals use square brackets. Reading a missing key isn't an
error and isn't a zero value — you get an Option back, and `??`
supplies the default. Iteration order is insertion order, always.
```rust
fn main() {
    let mut stock = ["apples": 4, "pears": 2]
    stock["plums"] = 10

    println(stock["apples"] ?? 0)
    println(stock["mangoes"] ?? 0)

    for (name, count) in stock {
        println("{name}: {count}")
    }
}
// 4
// 0
// apples: 4
// pears: 2
// plums: 10
```

`if let` unwraps the Option when you want to *do* something rather
than default: the binding exists only inside the braces.
```rust
fn main() {
    let menu = ["tea": 3]
    if let price = menu["tea"] {
        println("tea costs {price}")
    }
    if let price = menu["coffee"] {
        println("coffee costs {price}")
    } else {
        println("no coffee here")
    }
}
// tea costs 3
// no coffee here
```

## Match

`match` is the only multi-way branch, and it unifies Go's switch with
pattern matching. Arms take multiple values, ranges (half-open, like
`..` everywhere), and guards. No fallthrough exists, so no `break`
noise either. It's an expression, like `if`.
```rust
fn describe(code: Int) -> String {
    match code {
        200        => "ok"
        301, 302   => "redirect"
        400..500   => "client error"
        n if n < 0 => "not a status code"
        _          => "something else"
    }
}
fn main() {
    println(describe(200))
    println(describe(302))
    println(describe(418))
    println(describe(0 - 1))
}
// ok
// redirect
// client error
// not a status code
```

Strings match by equality — no regex smuggled into patterns.
```rust
fn main() {
    let verb = "POST"
    println(match verb {
        "GET"         => "read"
        "PUT", "POST" => "write"
        _             => "unknown"
    })
}
// write
```

Drop the subject and `match` becomes Go's expressionless switch: each
arm is a condition, the first true one wins. This replaces long
`else if` chains.
```rust
fn main() {
    let score = 87
    let grade = match {
        score >= 90 => "A"
        score >= 80 => "B"
        score >= 70 => "C"
        _           => "F"
    }
    println(grade)
}
// B
```
