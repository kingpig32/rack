exports.external = function(event, context) {

  process.on('uncaughtException', function(err) {
    return context.done(err);
  });

  var child = require('child_process').spawn('./bootstrap', [JSON.stringify(event)], { stdio:'inherit' })

  child.on('exit', function(code) {
    if (code !== 0 ) {
      return context.done(new Error("Process exited with non-zero status code: " + code));
    } else {
      context.done(null);
    }
  });
}
