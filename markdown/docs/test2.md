Sure! The classic **Fizz‑Buzz** problem is a great way to practice loops, conditionals, and string concatenation in Python.

### What the program should do

- Print the numbers 1 through *n* (often 100).
- For multiples of 3, print `"Fizz"` instead of the number.
- For multiples of 5, print `"Buzz"` instead of the number.
- For numbers that are multiples of both 3 and 5, print `"FizzBuzz"`.

---

## 1️⃣ Basic One‑liner (Python 3)

```python
for i in range(1, 101):
    print('Fizz'*(i%3==0) + 'Buzz'*(i%5==0) or i)
```

**How it works**

| Part | Explanation |
|------|-------------|
| `for i in range(1, 101)` | Loop from 1 to 100 inclusive. |
| `'Fizz'*(i%3==0)` | If `i` is divisible by 3, the boolean `True` evaluates to `1`, so the string is multiplied by 1 → `"Fizz"`. If not, `False` → `0`, so the string becomes `""`. |
| `'Buzz'*(i%5==0)` | Same logic for 5. |
| `+` | Concatenates the two strings. |
| `or i` | If the concatenated string is empty (i.e., not divisible by 3 or 5), the `or` operator returns `i`. |

---

## 2️⃣ Readable Function

If you prefer something more explicit and easier to modify:

```python
def fizz_buzz(n=100):
    """Print FizzBuzz from 1 to n."""
    for i in range(1, n + 1):
        output = ""
        if i % 3 == 0:
            output += "Fizz"
        if i % 5 == 0:
            output += "Buzz"
        print(output or i)

# Run it
fizz_buzz(100)
```

### Why this version is handy

- **Readability**: Each condition is clearly spelled out.
- **Extensibility**: Add more rules (e.g., "Bazz" for 7) with minimal changes.
- **Testing**: You can easily unit‑test the logic by capturing the output or returning a list.

---

## 3️⃣ Using a List Comprehension (for a quick return)

```python
def fizz_buzz_list(n=100):
    return [
        ("Fizz" if i % 3 == 0 else "") +
        ("Buzz" if i % 5 == 0 else "") or i
        for i in range(1, n + 1)
    ]

# Print each element
for line in fizz_buzz_list(30):
    print(line)
```

This returns a list of the results, which can be useful if you need to process them later.

---

## 4️⃣ Bonus: Using a Dictionary for Custom Rules

If you want a flexible, rule‑driven solution:

```python
def fizz_buzz_custom(n=100, rules=None):
    if rules is None:
        rules = {3: "Fizz", 5: "Buzz"}

    for i in range(1, n + 1):
        result = "".join(word for divisor, word in rules.items() if i % divisor == 0)
        print(result or i)

fizz_buzz_custom(30, {3: "Fizz", 5: "Buzz", 7: "Bazz"})
```

Now you can pass any set of divisors and strings.

---

### Quick recap

| Method | Best for |
|--------|----------|
| One‑liner | Quick scripts, coding contests |
| Function | Readability & testing |
| List comprehension | When you need a list of results |
| Dictionary | Customizable rules |

Feel free to copy any of these snippets into your Python environment and run them. Happy coding!