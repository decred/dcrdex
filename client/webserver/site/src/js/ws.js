// MessageSocket is a WebSocket manager that uses the Decred DEX Mesage format
// for communications.
//
// Message request format:
// {
//   route: 'name',
//   id: int,
//   payload: anything or nothing
// }
//
// Message response payload will be a result object with either a valid 'result'
// field or an 'error' field
//
// Functions for external use:
// registerRoute (route, handler) -- register a function to handle events
// of the given type
// request (route, payload) -- create a JSON message in the above format and
// send it
//
// Based on messagesocket_service.js by Jonathan Chappelow @ dcrdata, which is
// based on ws_events_dispatcher.js by Ismael Celis

const typeRequest = 1

function forward (route, payload, handlers) {
  if (!route && payload.error) {
    const err = payload.error
    console.error(`websocket error (code ${err.code}): ${err.message}`)
    return
  }
  if (typeof handlers[route] === 'undefined') {
    // console.log(`unhandled message for ${route}: ${payload}`)
    return
  }
  // call each handler
  for (var i = 0; i < handlers[route].length; i++) {
    handlers[route][i](payload)
  }
}

var id = 0

class MessageSocket {
  constructor () {
    this.uri = undefined
    this.connection = undefined
    this.handlers = {}
    this.queue = []
    this.maxQlength = 5
    this.connected = false
  }

  registerRoute (route, handler) {
    this.handlers[route] = this.handlers[route] || []
    this.handlers[route].push(handler)
  }

  deregisterRoute (route) {
    this.handlers[route] = []
  }

  // request sends a request-type message to the server
  request (route, payload) {
    if (!this.connected) {
      while (this.queue.length > this.maxQlength - 1) this.queue.shift()
      this.queue.push([route, payload])
      return
    }
    id++
    var message = JSON.stringify({
      route: route,
      type: typeRequest,
      id: id,
      payload: payload
    })

    if (window.loggingDebug) console.log('send', message)
    this.connection.send(message)
  }

  close (reason) {
    console.log('close, reason:', reason, this.handlers)
    this.handlers = {}
    this.connection.close()
  }

  connect (uri, reloader) {
    this.uri = uri
    this.reloader = reloader
    var retrys = 0
    const go = () => {
      const conn = this.connection = new window.WebSocket(uri)

      // unmarshal message, and forward the message to registered handlers
      conn.onmessage = (evt) => {
        var message = JSON.parse(evt.data)
        forward(message.route, message.payload, this.handlers)
      }

      // Stub out standard functions
      conn.onclose = () => {
        this.connected = false
        forward('close', null, this.handlers)
        retrys++
        // 1.2, 1.6, 2.0, 2.4, 3.1, 3.8, 4.8, 6.0, 7.5, 9.3, ...
        const delay = Math.min(Math.pow(1.25, retrys), 10)
        console.log(`websocket disconnected, trying again in ${delay.toFixed(1)} seconds`)
        setTimeout(() => {
          go()
        }, delay * 1000)
      }

      conn.onopen = () => {
        this.connected = true
        if (retrys > 0) {
          retrys = 0
          reloader()
        }
        forward('open', null, this.handlers)
        while (this.queue.length) {
          const [route, message] = this.queue.shift()
          this.request(route, message)
        }
      }

      conn.onerror = (evt) => {
        forward('error', evt, this.handlers)
      }
    }
    go()
  }
}

var ws = new MessageSocket()
export default ws
