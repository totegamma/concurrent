import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { Register } from './pages/Register'
import { Box, Paper } from '@mui/material'
import { Home } from './pages/Home'
import { Login } from './pages/Login'
import { Welcome } from './pages/Welcome'

function App(): JSX.Element {
    return (
        <Box sx={{width: '100vw', height: '100dvh', display: 'flex', justifyContent: 'center', padding: '30px', backgroundColor: '#333'}}>
            <Paper sx={{width: '900px', padding: '20px'}}>
                <BrowserRouter>
                    <Routes>
                        <Route path="/" element={<Home />} />
                        <Route path="/login" element={<Login />} />
                        <Route path="/welcome" element={<Welcome />} />
                        <Route path="/register" element={<Register />} />
                    </Routes>
                </BrowserRouter>
            </Paper>
        </Box>
    )
}

export default App
