let application

export function registerApplication (a) {
  application = a
}

export function app () {
  return application
}
