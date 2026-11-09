import { render } from 'preact'
import '@fontsource/fraunces/500.css'
import '@fontsource/instrument-sans/400.css'
import '@fontsource/instrument-sans/500.css'
import '@fontsource/instrument-sans/600.css'
import './styles.css'
import { App } from './App'

render(<App />, document.getElementById('app')!)
