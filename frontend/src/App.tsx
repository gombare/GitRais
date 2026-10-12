import {useEffect, useState} from 'react';
import './App.css';
import {Version} from "../wailsjs/go/app/App";

function App() {
    const [version, setVersion] = useState('');

    useEffect(() => {
        Version().then(setVersion);
    }, []);

    return (
        <div id="App">
            <h1>Gitrais</h1>
            <div id="result" className="result">v{version}</div>
        </div>
    )
}

export default App
