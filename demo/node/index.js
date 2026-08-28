const Sentry = require("@sentry/node");

const dsn = process.env.SENTRY_DSN || "http://public@127.0.0.1:9000/2";

Sentry.init({
  dsn: dsn,
  tracesSampleRate: 1.0,
  environment: "production",
  release: "node-demo@1.0.0",
  serverName: "node-worker-01",
});

Sentry.setUser({
  id: "node_user_102",
  email: "developer@nodejs.org",
  username: "nodeuser",
});

Sentry.addBreadcrumb({
  category: "lifecycle",
  message: "Node worker process initialized",
  level: "info",
});

function calculateTotal(items) {
  if (!items || items.length === 0) {
    throw new Error("Items array cannot be empty");
  }
  return items.reduce((sum, item) => sum + item.price, 0);
}

async function main() {
  console.log("Running Node.js Sentry SDK demo...");

  // 1. Capture error with stack trace
  try {
    calculateTotal([]);
  } catch (err) {
    Sentry.withScope((scope) => {
      scope.setTag("service", "checkout");
      scope.setTag("component", "cart_calculator");
      scope.setExtra("cart_id", "cart_9988");
      Sentry.captureException(err);
    });
    console.log("Captured error to Sentry:", err.message);
  }

  // 2. Capture info message
  Sentry.captureMessage("Node worker completed queue processing successfully", "info");

  // 3. Flush events before exiting
  await Sentry.flush(2000);
  console.log("Node.js demo finished successfully.");
}

main().catch((err) => {
  console.error("Unhandled rejection:", err);
  process.exit(1);
});

