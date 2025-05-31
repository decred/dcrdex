(() => {
  const ua = window.navigator.userAgent.toLowerCase()

  function getOS () {
    if (ua.includes('windows')) return 'windows'
    if (ua.includes('macintosh') || ua.includes('mac os x')) return 'mac'
    if (ua.includes('linux')) return 'linux'
    return 'unknown'
  }

  function getArchitecture () {
    if (ua.includes('arm')) return "arm"
    if (ua.includes('x86_64') || ua.includes('win64')) return "x86"
    return "unknown"
  }

  function idel (id) {
    return document.getElementById(id)
  }

  const osLogo = idel('osLogo')

  switch (getOS()) {
    case 'windows':
      osLogo.className = 'win-logo'
      break
    case 'linux':
      osLogo.className = 'linux-logo'
      break
    case 'mac':
      osLogo.className = 'mac-logo'
      break
    default:
      // Just assume they're on Windows, I guess?
      osLogo.className = 'win-logo'
  }
})()