// The page's behaviour: show the sum, and add to it. It asks for the sum when
// it loads, when it is looked at again, and with each addition. It does not
// poll: a request every few seconds would keep a sandbox bog awake.

const sum = document.getElementById("sum");
const form = document.getElementById("add");
const problem = document.getElementById("problem");

// Calls the API, and shows the sum it answers with, or what went wrong.
async function show(path, options) {
  try {
    const response = await fetch(path, options);
    const text = await response.text();
    if (!response.ok) throw new Error(problemIn(text));
    sum.textContent = JSON.parse(text).sum;
    problem.hidden = true;
  } catch (error) {
    problem.textContent = error.message;
    problem.hidden = false;
  }
}

// An error from the API is {"error": "..."}. One from the router in front of
// a deployed bog (too many requests, the bog is starting) is plain text.
function problemIn(text) {
  try {
    return JSON.parse(text).error ?? text;
  } catch {
    return text;
  }
}

form.addEventListener("submit", (event) => {
  event.preventDefault();
  show("/add", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ amount: Number(form.amount.value) }),
  });
});

document.addEventListener("visibilitychange", () => {
  if (!document.hidden) show("/sum");
});

show("/sum");
