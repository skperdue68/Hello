import './style.css';
import { GetHelloTime } from '../wailsjs/go/main/App';

const app = document.querySelector('#app');

async function updateTime() {
  try {
    const message = await GetHelloTime();
    app.innerHTML = `
      <main>
        <h1>${message}</h1>
        <p>Updates every 5 seconds.</p>
      </main>
    `;
  } catch (err) {
    app.innerHTML = `<main><h1>Error</h1><p>${err}</p></main>`;
  }
}

updateTime();
setInterval(updateTime, 5000);