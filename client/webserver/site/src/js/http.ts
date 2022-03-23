/*
 * requestJSON encodes the object and sends the JSON to the specified address.
 */
export async function requestJSON (method: string, addr: string, reqBody?: any): Promise<any> {
  try {
    const response = await window.fetch(addr, {
      method: method,
      headers: new window.Headers({ 'content-type': 'application/json' }),
      // credentials: "same-origin",
      body: reqBody
    })
    if (response.status !== 200) { throw response }
    const obj = await response.json()
    obj.requestSuccessful = true
    return obj
  } catch (response) {
    response.requestSuccessful = false
    response.msg = await response.text()
    return response
  }
}

/*
 * postJSON sends a POST request with JSON-formatted data and returns the
 * response.
 */
export async function postJSON (addr: string, data?: any) {
  return requestJSON('POST', addr, JSON.stringify(data))
}

/*
 * getJSON sends a GET request and returns the response.
 */
export async function getJSON (addr: string): Promise<any> {
  return requestJSON('GET', addr)
}
